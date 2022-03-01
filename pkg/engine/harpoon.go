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
	kubeMethod         = "kube"
	systemdMethod      = "systemd"
	fileTransferMethod = "filetransfer"
)

// HarpoonConfig requires necessary objects to process targets
type HarpoonConfig struct {
	Repos []api.Repo `mapstructure:"repos"`

	// map of repoName to map of method:schedule
	RepoTargetMap map[string]map[string]string
	Volume        string
	configFile    string // "./config.yaml"
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
	o.Repos = config.Repos
}

// getTargets returns map of repoName to map of method:Schedule
func (hc *HarpoonConfig) getTargets() {
	RepoTargetMap := make(map[string]map[string]string)
	var targets []interface{}
	for _, repo := range hc.Repos {
		schedMap := make(map[string]string)
		// TODO: this should not be hard-coded, in the future might allow for arbitrary target types with an interface
		targets = append(targets, repo.Target.Raw, repo.Target.Systemd, repo.Target.Kube, repo.Target.FileTransfer)
		for _, i := range targets {
			switch i.(type) {
			case api.Raw:
				if repo.Target.Raw.Schedule == "" {
					continue
				}
				schedMap[rawMethod] = repo.Target.Raw.Schedule
			case api.Kube:
				if repo.Target.Kube.Schedule == "" {
					continue
				}
				schedMap[kubeMethod] = repo.Target.Kube.Schedule
			case api.Systemd:
				if repo.Target.Systemd.Schedule == "" {
					continue
				}
				schedMap[systemdMethod] = repo.Target.Systemd.Schedule
			case api.FileTransfer:
				if repo.Target.FileTransfer.Schedule == "" {
					continue
				}
				schedMap[fileTransferMethod] = repo.Target.FileTransfer.Schedule
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
	// TODO there must be a better way
	var split = "-SPLIT-"
	allTargets := make(map[string]string)
	for _, repo := range hc.Repos {
		directory := filepath.Base(repo.Target.Url)
		if err := hc.getClone(&repo, directory); err != nil {
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
		var repo api.Repo
		for _, r := range hc.Repos {
			if repoName == r.Name {
				repo = r
			}
		}
		switch method {
		case kubeMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Target.Kube.InitialRun = true
			s.Cron(schedule).Do(hc.processKube, ctx, &repo, schedule)
		case rawMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Target.Raw.InitialRun = true
			s.Cron(schedule).Do(hc.processRaw, ctx, &repo, schedule)
		case systemdMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Target.Systemd.InitialRun = true
			s.Cron(schedule).Do(hc.processSystemd, ctx, &repo, schedule)
		case fileTransferMethod:
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			klog.Infof("Processing Repo: %s Method: %s", repo.Name, method)
			repo.Target.FileTransfer.InitialRun = true
			s.Cron(schedule).Do(hc.processFileTransfer, ctx, &repo, schedule)
		default:
			log.Fatalf("unknown target method")
		}
	}
	s.StartAsync()
	select {}
}

func (hc *HarpoonConfig) processRaw(ctx context.Context, repo *api.Repo, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()

	initial := repo.Target.Raw.InitialRun
	repo.Target.Raw.InitialRun = false
	directory := filepath.Base(repo.Target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, rawMethod, err)
	}
	if initial {
		tree, err := getTree(gitRepo)
		if err != nil {
			log.Fatal(err)
		}

		subDir := repo.Target.Raw.Subdirectory
		subDirTree, err := tree.Tree(subDir)
		if err != nil {
			log.Fatalf("Repo: %s, Method: %s error when switching to subdirectory tree for directory %s: %s", repo.Name, rawMethod, subDir, err)
		}

		// ... get the files iterator and print the file
		// .. make sure we're only calling the raw engine method on json files
		subDirTree.Files().ForEach(func(f *object.File) error {
			if strings.HasSuffix(f.Name, ".json") {
				path := filepath.Join(directory, subDir, f.Name)
				if err := hc.EngineMethod(ctx, path, rawMethod, repo); err != nil {
					return err
				}
			}
			return nil
		})
	}
	changes, isDiff := hc.findDiff(gitRepo, directory, rawMethod, repo.Target.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, rawMethod)
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, repo.Target.Raw.Subdirectory) {
			path := directory + "/" + change.To.Name
			if err = hc.EngineMethod(ctx, path, rawMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processSystemd(ctx context.Context, repo *api.Repo, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.Target.Systemd.InitialRun
	repo.Target.Systemd.InitialRun = false
	directory := filepath.Base(repo.Target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, systemdMethod, err)
	}
	if initial {
		tree, err := getTree(gitRepo)
		if err != nil {
			log.Fatal(err)
		}

		subDir := repo.Target.Systemd.Subdirectory
		subDirTree, err := tree.Tree(subDir)
		if err != nil {
			log.Fatalf("Repo: %s, Method: %s error when switching to subdirectory tree for directory %s: %s", repo.Name, systemdMethod, subDir, err)
		}

		// ... get the files iterator and print the file
		// .. make sure we're only calling the raw engine method on json files
		subDirTree.Files().ForEach(func(f *object.File) error {
			if strings.HasSuffix(f.Name, ".service") {
				path := filepath.Join(directory, subDir, f.Name)
				if err := hc.EngineMethod(ctx, path, systemdMethod, repo); err != nil {
					return err
				}
			}
			return nil
		})
	}

	changes, isDiff := hc.findDiff(gitRepo, directory, systemdMethod, repo.Target.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, systemdMethod)
		return
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, repo.Target.Systemd.Subdirectory) {
			path := directory + "/" + change.To.Name
			if err = hc.EngineMethod(ctx, path, systemdMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processFileTransfer(ctx context.Context, repo *api.Repo, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.Target.FileTransfer.InitialRun
	repo.Target.FileTransfer.InitialRun = false
	directory := filepath.Base(repo.Target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, fileTransferMethod, err)
	}
	if initial {
		tree, err := getTree(gitRepo)
		if err != nil {
			log.Fatal(err)
		}

		subDir := repo.Target.FileTransfer.Subdirectory
		subDirTree, err := tree.Tree(subDir)
		if err != nil {
			log.Fatalf("Repo: %s, Method: %s error when switching to subdirectory tree for directory %s: %s", repo.Name, fileTransferMethod, subDir, err)
		}

		// ... get the files iterator and print the file
		// .. make sure we're only calling the raw engine method on json files
		subDirTree.Files().ForEach(func(f *object.File) error {
			path := filepath.Join(directory, subDir, f.Name)
			if err := hc.EngineMethod(ctx, path, fileTransferMethod, repo); err != nil {
				return err
			}
			return nil
		})
	}

	changes, isDiff := hc.findDiff(gitRepo, directory, fileTransferMethod, repo.Target.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, fileTransferMethod)
		return
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, repo.Target.FileTransfer.Subdirectory) {
			path := directory + "/" + change.To.Name
			if err = hc.EngineMethod(ctx, path, fileTransferMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) processKube(ctx context.Context, repo *api.Repo, schedule string) {
	repo.Mu.Lock()
	defer repo.Mu.Unlock()
	initial := repo.Target.Kube.InitialRun
	repo.Target.Kube.InitialRun = false
	directory := filepath.Base(repo.Target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", repo.Name, kubeMethod, err)
	}
	if initial {
		tree, err := getTree(gitRepo)
		if err != nil {
			log.Fatal(err)
		}

		subDir := repo.Target.Kube.Subdirectory
		subDirTree, err := tree.Tree(subDir)
		if err != nil {
			log.Fatalf("Repo: %s, Method: %s error when switching to subdirectory tree for directory %s: %s", repo.Name, kubeMethod, subDir, err)
		}

		// ... get the files iterator and print the file
		// .. make sure we're only calling the raw engine method on json files
		subDirTree.Files().ForEach(func(f *object.File) error {
			if strings.HasSuffix(f.Name, ".yaml") || strings.HasSuffix(f.Name, ".yml") {
				path := filepath.Join(directory, subDir, f.Name)
				if err := hc.EngineMethod(ctx, path, kubeMethod, repo); err != nil {
					return err
				}
			}
			return nil
		})
	}
	changes, isDiff := hc.findDiff(gitRepo, directory, kubeMethod, repo.Target.Branch, initial)
	if !isDiff && !initial {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", repo.Name, kubeMethod)
		return
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, repo.Target.Kube.Subdirectory) {
			path := directory + "/" + change.To.Name
			if err = hc.EngineMethod(ctx, path, kubeMethod, repo); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(repo)
}

func (hc *HarpoonConfig) update(repo *api.Repo) {
	for _, r := range hc.Repos {
		if repo.Name == r.Name {
			r = *repo
		}
	}
}

func (hc *HarpoonConfig) findDiff(gitRepo *git.Repository, directory, method, branch string, firstRun bool) ([]*object.Change, bool) {
	var err error
	if gitRepo == nil {
		gitRepo, err = git.PlainOpen(directory)
		if err != nil {
			log.Fatalf("Repo: %s, error while opening the repository: %s", directory, err)
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

func (hc *HarpoonConfig) EngineMethod(ctx context.Context, path, method string, repo *api.Repo) error {
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
func (hc *HarpoonConfig) getClone(repo *api.Repo, directory string) error {
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
		klog.Infof("git clone %s %s --recursive", repo.Target.Url, repo.Target.Branch)
		var user string
		if repo.PAT != "" {
			user = "harpoon"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user,
				Password: repo.PAT,
			},
			URL:           repo.Target.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", repo.Target.Branch)),
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
