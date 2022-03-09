package engine

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/redhat-et/harpoon/pkg/engine/api"

	"k8s.io/klog/v2"
)

// TODO create interface for Method type, so can plug in arbitrary method types
const (
	harpoon           = "harpoon"
	defaultConfigFile = "./config.yaml"
	defaultVolume     = "harpoon-volume"
	harpoonImage      = "quay.io/harpoon/harpoon:latest"

	rawMethod          = "raw"
	systemdMethod      = "systemd"
	kubeMethod         = "kube"
	fileTransferMethod = "filetransfer"
)

// HarpoonConfig requires necessary objects to process targets
type HarpoonConfig struct {
	Targets []*api.Target `mapstructure:"targets"`
	PAT     string        `mapstructure:"pat"`

	// map of Target.Name to map of method:schedule
	RepoTargetMap map[string]map[string]string
	Volume        string
	configFile    string // "currently not configurable, ./config.yaml"
}

func NewHarpoonConfig() *HarpoonConfig {
	return &HarpoonConfig{
		RepoTargetMap: make(map[string]map[string]string),
	}
}

var harpoonConfig *HarpoonConfig
var harpoonVolume string

// harpoonCmd represents the base command when called without any subcommands
var harpoonCmd = &cobra.Command{
	Version: "0.0.0",
	Use:     harpoon,
	Short:   "a tool to schedule gitOps workflows",
	Long:    "Harpoon is a tool to schedule gitOps workflows based on a given configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags
// appropriately. This is called by main.main().
func Execute() {
	cobra.CheckErr(harpoonCmd.Execute())
}

func (o *HarpoonConfig) bindFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.configFile, "config", defaultConfigFile, "file that holds harpoon configuration")
	flags.StringVar(&o.Volume, "volume", defaultVolume, "podman volume to hold harpoon data. If volume doesn't exist, harpoon will create it.")
	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

// initconfig reads in config file and env variables if set.
func (o *HarpoonConfig) initConfig(cmd *cobra.Command) {
	v := viper.New()
	if o.configFile == "" {
		o.configFile = defaultConfigFile
	}
	flagConfigDir := filepath.Dir(o.configFile)
	flagConfigName := filepath.Base(o.configFile)
	v.AddConfigPath(flagConfigDir)
	v.SetConfigName(flagConfigName)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err != nil {
		cobra.CheckErr(fmt.Errorf("fatal error using config file %s. %w \n", o.configFile, err))
	}
	var config = NewHarpoonConfig()
	klog.Infof("Using config file: %s", v.ConfigFileUsed())
	if err := v.Unmarshal(&config); err != nil {
		cobra.CheckErr(err)
	}

	if config.Volume == "" {
		config.Volume = defaultVolume
	}

	harpoonVolume = config.Volume
	o.Targets = config.Targets
}

// getTargets returns map of repoName to map of method:Schedule
func (hc *HarpoonConfig) getTargets() {
	RepoTargetMap := make(map[string]map[string]string)
	var targets []interface{}
	for _, repo := range hc.Targets {
		schedMap := make(map[string]string)
		// TODO: this should not be hard-coded, in the future might allow for arbitrary target types with an interface
		targets = append(targets, repo.Raw, repo.Systemd, repo.Kube, repo.FileTransfer)
		for _, i := range targets {
			switch i.(type) {
			case api.Raw:
				if repo.Raw.Schedule == "" {
					continue
				}
				schedMap[rawMethod] = repo.Raw.Schedule
			case api.Kube:
				if repo.Kube.Schedule == "" {
					continue
				}
				schedMap[kubeMethod] = repo.Kube.Schedule
			case api.Systemd:
				if repo.Systemd.Schedule == "" {
					continue
				}
				schedMap[systemdMethod] = repo.Systemd.Schedule
			case api.FileTransfer:
				if repo.FileTransfer.Schedule == "" {
					continue
				}
				schedMap[fileTransferMethod] = repo.FileTransfer.Schedule
			default:
				log.Fatalf("unknown target method")
			}
		}
		RepoTargetMap[repo.Name] = schedMap
	}
	hc.RepoTargetMap = RepoTargetMap
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (hc *HarpoonConfig) runTargets() {
	hc.getTargets()
	allTargets := make(map[string]string)
	var split = "-SPLIT-"
	for _, repo := range hc.Targets {
		directory := filepath.Base(repo.Url)
		if err := hc.getClone(repo, directory); err != nil {
			log.Fatal(err)
		}
		for method, schedule := range hc.RepoTargetMap[repo.Name] {
			allTargets[fmt.Sprintf("%s%s%s", method, split, repo.Name)] = schedule
		}
	}
	// TODO: Fix this, it works, but look at it
	// The only way I could get this to loop thru Repos is to have all targets in a
	// single map.
	s := gocron.NewScheduler(time.UTC)
	// TODO: can tag jobs, use tags to track
	for methodRepo, schedule := range allTargets {
		methodAndRepo := strings.Split(methodRepo, split)
		method := methodAndRepo[0]
		repoName := methodAndRepo[1]
		var repo api.Target
		for _, r := range hc.Targets {
			if repoName == r.Name {
				repo = *r
			}
		}
		switch method {
		case kubeMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Kube.InitialRun = true
			s.Cron(schedule).Do(hc.processKube, ctx, &repo, schedule)
		case rawMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Raw.InitialRun = true
			s.Cron(schedule).Do(hc.processRaw, ctx, &repo, schedule)
		case systemdMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Systemd.InitialRun = true
			s.Cron(schedule).Do(hc.processSystemd, ctx, &repo, schedule)
		case fileTransferMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.FileTransfer.InitialRun = true
			s.Cron(schedule).Do(hc.processFileTransfer, ctx, &repo, schedule)
		default:
			log.Fatalf("unknown target method")
		}
	}
	s.StartAsync()
	select {}
}

func (hc *HarpoonConfig) processRaw(ctx context.Context, repo *api.Target, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()

	initial := repo.Raw.InitialRun
	repo.Raw.InitialRun = false
	directory := filepath.Base(repo.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, rawMethod, err)
	}
	var targetFile = ""
	tag := ".json"
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, repo.Raw.TargetPath, rawMethod, repo)
		if err != nil {
			log.Fatal(err)
		}
		if fileName != "" {
			targetFile = fileName
			if !strings.HasSuffix(fileName, tag) {
				log.Fatalf("%s target file must be of type %s", rawMethod, tag)
			}
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, path, rawMethod, repo); err != nil {
				log.Fatal(err)
			}
		} else {
			// ... get the files iterator and print the file
			// .. make sure we're only calling the raw engine method on json files
			subDirTree.Files().ForEach(func(f *object.File) error {
				if strings.HasSuffix(f.Name, tag) {
					path := filepath.Join(directory, repo.Raw.TargetPath, f.Name)
					if err := hc.EngineMethod(ctx, path, rawMethod, repo); err != nil {
						return err
					}
				}
				return nil
			})
		}
	}
	changes, isDiff := hc.findDiff(gitRepo, directory, rawMethod, repo.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, rawMethod)
	}

	tp := repo.Raw.TargetPath
	if targetFile != "" {
		tp = targetFile
	}
	for _, change := range changes {
		if strings.Contains(change.To.Name, tp) {
			path := directory + "/" + change.To.Name
			if err := hc.EngineMethod(ctx, path, rawMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processSystemd(ctx context.Context, repo *api.Target, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.Systemd.InitialRun
	repo.Systemd.InitialRun = false
	directory := filepath.Base(repo.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, systemdMethod, err)
	}
	var targetFile = ""
	tag := ".service"
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, repo.Systemd.TargetPath, systemdMethod, repo)
		if err != nil {
			log.Fatal(err)
		}
		if fileName != "" {
			targetFile = fileName
			if !strings.HasSuffix(fileName, tag) {
				log.Fatalf("%s target file must be of type %s", systemdMethod, tag)
			}
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, path, systemdMethod, repo); err != nil {
				log.Fatal(err)
			}
		} else {
			// ... get the files iterator and print the file
			// .. make sure we're only calling the raw engine method on json files
			subDirTree.Files().ForEach(func(f *object.File) error {
				if strings.HasSuffix(f.Name, tag) {
					path := filepath.Join(directory, repo.Systemd.TargetPath, f.Name)
					if err := hc.EngineMethod(ctx, path, systemdMethod, repo); err != nil {
						return err
					}
				}
				return nil
			})
		}
	}

	changes, isDiff := hc.findDiff(gitRepo, directory, systemdMethod, repo.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, systemdMethod)
		return
	}

	tp := repo.Systemd.TargetPath
	if targetFile != "" {
		tp = targetFile
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, tp) {
			path := directory + "/" + change.To.Name
			if err := hc.EngineMethod(ctx, path, systemdMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processFileTransfer(ctx context.Context, repo *api.Target, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.FileTransfer.InitialRun
	repo.FileTransfer.InitialRun = false
	directory := filepath.Base(repo.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, fileTransferMethod, err)
	}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, repo.FileTransfer.TargetPath, fileTransferMethod, repo)
		if err != nil {
			log.Fatal(err)
		}
		if fileName != "" {
			targetFile = fileName
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, path, fileTransferMethod, repo); err != nil {
				log.Fatal(err)
			}
		} else {
			// ... get the files iterator and print the file
			// .. make sure we're only calling the raw engine method on json files
			subDirTree.Files().ForEach(func(f *object.File) error {
				path := filepath.Join(directory, repo.FileTransfer.TargetPath, f.Name)
				if err := hc.EngineMethod(ctx, path, fileTransferMethod, repo); err != nil {
					return err
				}
				return nil
			})
		}
	}

	changes, isDiff := hc.findDiff(gitRepo, directory, fileTransferMethod, repo.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, fileTransferMethod)
		return
	}

	tp := repo.FileTransfer.TargetPath
	if targetFile != "" {
		tp = targetFile
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, tp) {
			path := directory + "/" + change.To.Name
			if err := hc.EngineMethod(ctx, path, fileTransferMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processKube(ctx context.Context, repo *api.Target, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.Kube.InitialRun
	repo.Kube.InitialRun = false
	directory := filepath.Base(repo.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, kubeMethod, err)
	}
	tag := []string{"yaml", "yml"}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, repo.Kube.TargetPath, kubeMethod, repo)
		if err != nil {
			log.Fatal(err)
		}
		if fileName != "" {
			targetFile = fileName
			found := false
			for _, ft := range tag {
				if strings.HasSuffix(fileName, ft) {
					found = true
					path := filepath.Join(directory, fileName)
					if err := hc.EngineMethod(ctx, path, kubeMethod, repo); err != nil {
						log.Fatal(err)
					}
				}
			}
			if !found {
				log.Fatalf("%s target file must be of type %v", kubeMethod, tag)
			}

		} else {
			// ... get the files iterator and print the file
			// .. make sure we're only calling the raw engine method on json files
			subDirTree.Files().ForEach(func(f *object.File) error {
				if strings.HasSuffix(f.Name, tag[0]) || strings.HasSuffix(f.Name, tag[1]) {
					path := filepath.Join(directory, repo.Kube.TargetPath, f.Name)
					if err := hc.EngineMethod(ctx, path, kubeMethod, repo); err != nil {
						return err
					}
				}
				return nil
			})
		}
	}
	changes, isDiff := hc.findDiff(gitRepo, directory, kubeMethod, repo.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, kubeMethod)
		return
	}

	tp := repo.Kube.TargetPath
	if targetFile != "" {
		tp = targetFile
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, tp) {
			path := directory + "/" + change.To.Name
			if err := hc.EngineMethod(ctx, path, kubeMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) update(repo *api.Target) {
	for _, r := range hc.Targets {
		if repo.Name == r.Name {
			r = repo
		}
	}
}

func (hc *HarpoonConfig) findDiff(gitRepo *git.Repository, directory, method, branch string, firstRun bool) ([]*object.Change, bool) {
	var err error
	if gitRepo == nil {
		gitRepo, err = git.PlainOpen(directory)
		if err != nil {
			log.Fatalf("error while opening the repository: %s: %s", directory, err)
		}
	}
	w, err := gitRepo.Worktree()
	if err != nil {
		log.Fatalf("Error while opening the worktree: %s\n", err)
	}
	// ... retrieve the tree from the commit
	prevTree, err := getTree(gitRepo)
	if err != nil {
		log.Fatal(err)
	}
	// Pull the latest changes from the origin remote and merge into the current branch
	if err = w.Pull(&git.PullOptions{
		ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", branch)),
		SingleBranch:  true,
	}); err != nil && !firstRun {
		return nil, false
	}
	tree, err := getTree(gitRepo)
	if err != nil {
		log.Fatal(err)
	}
	changes, err := tree.Diff(prevTree)
	if err != nil {
		log.Fatalf("Error while generating diff: %s\n", err)
	}
	return changes, firstRun
}

func (hc *HarpoonConfig) EngineMethod(ctx context.Context, path, method string, repo *api.Target) error {
	switch method {
	case rawMethod:
		if err := RawPodman(ctx, path); err != nil {
			return err
		}
	case systemdMethod:
		if err := SystemdPodman(ctx, path, repo.Name); err != nil {
			return err
		}
		// TODO
	case fileTransferMethod:
		klog.Infof("Called FileTransfer Method, returning nil, since we haven't written the logic yet")
		return nil
		// TODO
	case kubeMethod:
		if err := kubePodman(ctx, path); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
	return nil
}

// This assumes unique urls - only 1 repo per "directory"
func (hc *HarpoonConfig) getClone(repo *api.Target, directory string) error {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	absPath, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("Repo: %s, error while fetching local clone: %s", repo.Name, err)
	}
	var exists bool
	if _, err := os.Stat(directory); err == nil {
		exists = true
		// if directory/.git does not exist, fail quickly
		if _, err := os.Stat(directory + "/.git"); err != nil {
			return fmt.Errorf("%s exists but is not a git repository", directory)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("error retrieving git repository: %s: %v", directory, err)
	}

	if !exists {
		// TODO: Allow multiple branches per repository
		// Will need to add a Checkout per branch, per target
		klog.Infof("git clone %s %s --recursive", repo.Url, repo.Branch)
		var user string
		if hc.PAT != "" {
			user = "harpoon"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user,
				Password: hc.PAT,
			},
			URL:           repo.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			return fmt.Errorf("Error while cloning the repository: %s", err)
		}
	}
	return nil
}

func getTree(r *git.Repository) (*object.Tree, error) {
	ref, err := r.Head()
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving head: %s\n", err)
	}
	// ... retrieving the commit object
	commit, err := r.CommitObject(ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving commit: %s\n", err)
	}
	// ... retrieve the tree from the commit
	tree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("Error when retrieving tree: %s\n", err)
	}
	return tree, nil
}

func getPathOrTree(directory, subDir, method string, repo *api.Target) (string, *object.Tree, error) {
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s, error while opening the repository: %s", directory, err)
	}
	tree, err := getTree(gitRepo)
	if err != nil {
		return "", nil, err
	}

	subDirTree, err := tree.Tree(subDir)
	if err != nil {
		if err == object.ErrDirectoryNotFound {
			// check if exact filepath
			file, err := tree.File(subDir)
			if err == nil {
				return file.Name, nil, nil
			}
		}
	}
	return "", subDirTree, err
}
