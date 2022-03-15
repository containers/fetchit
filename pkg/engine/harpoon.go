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
	ansibleMethod      = "ansible"
)

// HarpoonConfig requires necessary objects to process targets
type HarpoonConfig struct {
	Targets []*api.Target `mapstructure:"targets"`
	PAT     string        `mapstructure:"pat"`

	Volume     string
	configFile string // "currently not configurable, ./config.yaml"
}

func NewHarpoonConfig() *HarpoonConfig {
	return &HarpoonConfig{
		Targets: []*api.Target{
			{
				MethodSchedules: make(map[string]string),
			},
		},
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
	var methods []interface{}
	for _, target := range hc.Targets {
		schedMethods := make(map[string]string)
		// TODO: this should not be hard-coded, in the future might allow for arbitrary target types with an interface
		methods = append(methods, target.Raw, target.Systemd, target.Kube, target.FileTransfer, target.Ansible)
		for _, i := range methods {
			switch i.(type) {
			case api.Raw:
				if target.Raw.Schedule == "" {
					continue
				}
				schedMethods[rawMethod] = target.Raw.Schedule
			case api.Kube:
				if target.Kube.Schedule == "" {
					continue
				}
				schedMethods[kubeMethod] = target.Kube.Schedule
			case api.Systemd:
				if target.Systemd.Schedule == "" {
					continue
				}
				schedMethods[systemdMethod] = target.Systemd.Schedule
			case api.FileTransfer:
				if target.FileTransfer.Schedule == "" {
					continue
				}
				schedMethods[fileTransferMethod] = target.FileTransfer.Schedule
			case api.Ansible:
				if target.Ansible.Schedule == "" {
					continue
				}
				schedMethods[ansibleMethod] = target.Ansible.Schedule
			default:
				log.Fatalf("unknown target method")
			}
		}
		target.MethodSchedules = schedMethods
	}
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (hc *HarpoonConfig) runTargets() {
	hc.getTargets()
	allTargets := make(map[string]map[string]string)
	for _, target := range hc.Targets {
		directory := filepath.Base(target.Url)
		if err := hc.getClone(target, directory); err != nil {
			log.Fatal(err)
		}
		allTargets[target.Name] = target.MethodSchedules
	}

	s := gocron.NewScheduler(time.UTC)
	for repoName, schedMethods := range allTargets {
		var target api.Target
		for _, t := range hc.Targets {
			if repoName == t.Name {
				target = *t
			}
		}
		for method, schedule := range schedMethods {
			switch method {
			case kubeMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Kube.InitialRun = true
				s.Cron(schedule).Do(hc.processKube, ctx, &target, schedule)
			case rawMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Raw.InitialRun = true
				s.Cron(schedule).Do(hc.processRaw, ctx, &target, schedule)
			case systemdMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Systemd.InitialRun = true
				s.Cron(schedule).Do(hc.processSystemd, ctx, &target, schedule)
			case fileTransferMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.FileTransfer.InitialRun = true
				s.Cron(schedule).Do(hc.processFileTransfer, ctx, &target, schedule)
			case ansibleMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Ansible.InitialRun = true
				s.Cron(schedule).Do(hc.processAnsible, ctx, &target, schedule)
			default:
				log.Fatalf("unknown target method")
			}
		}
	}
	s.StartAsync()
	select {}
}

func (hc *HarpoonConfig) processRaw(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()

	initial := target.Raw.InitialRun
	target.Raw.InitialRun = false
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", target.Name, rawMethod, err)
	}
	var targetFile = ""
	tag := []string{".json"}
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, target.Raw.TargetPath, rawMethod, target)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, &tag, directory, target, subDirTree, target.Raw.TargetPath, rawMethod)
		if err != nil {
			log.Fatal(err)
		}
	}

	hc.getChangesAndRunEngine(ctx, gitRepo, directory, rawMethod, target, targetFile, target.Raw.TargetPath)
}

func (hc *HarpoonConfig) processAnsible(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Ansible.InitialRun
	target.Ansible.InitialRun = false
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", target.Name, ansibleMethod, err)
	}
	tag := []string{"yaml", "yml"}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, target.Ansible.TargetPath, ansibleMethod, target)
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
					if err := hc.EngineMethod(ctx, path, ansibleMethod, target); err != nil {
						log.Fatal(err)
					}
				}
			}
			if !found {
				log.Fatalf("%s target file must be of type %v", kubeMethod, tag)
			}

		} else {
			// ... get the files iterator and print the file
			subDirTree.Files().ForEach(func(f *object.File) error {
				if strings.HasSuffix(f.Name, tag[0]) || strings.HasSuffix(f.Name, tag[1]) {
					path := filepath.Join(directory, target.Ansible.TargetPath, f.Name)
					if err := hc.EngineMethod(ctx, path, ansibleMethod, target); err != nil {
						return err
					}
				}
				return nil
			})
		}
	}

	changes := hc.findDiff(gitRepo, directory, ansibleMethod, target.Branch)
	if changes == nil {
		hc.update(target)
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", target.Name, ansibleMethod)
		return
	}

	tp := target.Ansible.TargetPath
	if targetFile != "" {
		tp = targetFile
	}

	for _, change := range changes {
		if strings.Contains(change.To.Name, tp) {
			path := directory + "/" + change.To.Name
			if err := hc.EngineMethod(ctx, path, ansibleMethod, target); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(target)
}

func (hc *HarpoonConfig) processSystemd(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Systemd.InitialRun
	target.Systemd.InitialRun = false
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", target.Name, systemdMethod, err)
	}
	var targetFile = ""
	tag := []string{".service"}
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, target.Systemd.TargetPath, systemdMethod, target)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, &tag, directory, target, subDirTree, target.Systemd.TargetPath, systemdMethod)
		if err != nil {
			log.Fatal(err)
		}
	}

	hc.getChangesAndRunEngine(ctx, gitRepo, directory, systemdMethod, target, targetFile, target.Systemd.TargetPath)
}

func (hc *HarpoonConfig) processFileTransfer(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.FileTransfer.InitialRun
	target.FileTransfer.InitialRun = false
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", target.Name, fileTransferMethod, err)
	}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, target.FileTransfer.TargetPath, fileTransferMethod, target)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, nil, directory, target, subDirTree, target.FileTransfer.TargetPath, fileTransferMethod)
		if err != nil {
			log.Fatal(err)
		}
	}

	hc.getChangesAndRunEngine(ctx, gitRepo, directory, fileTransferMethod, target, targetFile, target.FileTransfer.TargetPath)
}

func (hc *HarpoonConfig) processKube(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Kube.InitialRun
	target.Kube.InitialRun = false
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s Method: %s, error while opening the repository: %s", target.Name, kubeMethod, err)
	}
	tag := []string{"yaml", "yml"}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := getPathOrTree(directory, target.Kube.TargetPath, kubeMethod, target)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, &tag, directory, target, subDirTree, target.Kube.TargetPath, kubeMethod)
		if err != nil {
			log.Fatal(err)
		}
	}

	hc.getChangesAndRunEngine(ctx, gitRepo, directory, kubeMethod, target, targetFile, target.Kube.TargetPath)
}

func (hc *HarpoonConfig) applyInitial(ctx context.Context, fileName string, tag *[]string, directory string, target *api.Target, subDirTree *object.Tree, tp string, method string) (string, error) {
	if fileName != "" {
		found := false
		if hc.checkTag(tag, fileName) {
			found = true
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, path, method, target); err != nil {
				return fileName, err
			}
		}
		if !found {
			log.Fatalf("%s target file must be of type %v", method, tag)
		}

	} else {
		// ... get the files iterator and print the file
		err := subDirTree.Files().ForEach(func(f *object.File) error {
			if hc.checkTag(tag, f.Name) {
				path := filepath.Join(directory, tp, f.Name)
				if err := hc.EngineMethod(ctx, path, method, target); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fileName, nil
		}
	}
	return fileName, nil
}

func (hc *HarpoonConfig) checkTag(tags *[]string, name string) bool {
	if tags == nil {
		return true
	}
	for _, tag := range *tags {
		if strings.HasSuffix(name, tag) {
			return true
		}
	}
	return false
}

func (hc *HarpoonConfig) getChangesAndRunEngine(ctx context.Context, gitRepo *git.Repository, directory string, method string, target *api.Target, targetFile string, targetPath string) {
	changes := hc.findDiff(gitRepo, directory, method, target.Branch)

	if changes == nil {
		hc.update(target)
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", target.Name, method)
		return
	}

	tp := targetPath
	if targetFile != "" {
		tp = targetFile
	}

	// the change logic is backwards "From" is actually "To"
	for _, change := range changes {
		if strings.Contains(change.From.Name, tp) {
			path := directory + "/" + change.From.Name
			if err := hc.EngineMethod(ctx, path, kubeMethod, target); err != nil {
				log.Fatal(err)
			}
		}
	}
	hc.update(target)
}

func (hc *HarpoonConfig) update(target *api.Target) {
	for _, t := range hc.Targets {
		if target.Name == t.Name {
			t = target
		}
	}
}

func (hc *HarpoonConfig) findDiff(gitRepo *git.Repository, directory, method, branch string) []*object.Change {
	w, err := gitRepo.Worktree()
	if err != nil {
		log.Fatalf("error while opening the worktree: %s\n", err)
	}
	// ... retrieve the tree from the commit
	prevTree, err := getTree(gitRepo)
	if err != nil {
		log.Fatal(err)
	}
	// Pull the latest changes from the origin remote and merge into the current branch
	ref := fmt.Sprintf("refs/heads/%s", branch)
	if err = w.Pull(&git.PullOptions{
		ReferenceName: plumbing.ReferenceName(ref),
		SingleBranch:  true,
	}); err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return nil
		}
		log.Fatalf("%s: error while pulling in latest changes from %s", directory, ref)
	}
	tree, err := getTree(gitRepo)
	if err != nil {
		log.Fatal(err)
	}
	changes, err := tree.Diff(prevTree)
	if err != nil {
		log.Fatalf("%s: error while generating diff: %s", directory, err)
	}
	return changes
}

func (hc *HarpoonConfig) EngineMethod(ctx context.Context, path, method string, target *api.Target) error {
	// TODO: make processMethod interface, to add arbitrary methods
	switch method {
	case rawMethod:
		return rawPodman(ctx, path)
	case systemdMethod:
		// TODO: add logic for non-root services
		dest := "/etc/systemd/system"
		return systemdPodman(ctx, path, dest, target)
	case fileTransferMethod:
		dest := target.FileTransfer.DestinationDirectory
		return fileTransferPodman(ctx, path, dest, fileTransferMethod, target)
	case kubeMethod:
		return kubePodman(ctx, path)
	case ansibleMethod:
		return ansiblePodman(ctx, path, target.Name, target.Ansible.SshDirectory)
	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
}

// This assumes unique urls - only 1 git repo per "directory"
func (hc *HarpoonConfig) getClone(target *api.Target, directory string) error {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	absPath, err := filepath.Abs(directory)
	if err != nil {
		return fmt.Errorf("Repo: %s, error while fetching local clone: %s", target.Name, err)
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
		klog.Infof("git clone %s %s --recursive", target.Url, target.Branch)
		var user string
		if hc.PAT != "" {
			user = "harpoon"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user,
				Password: hc.PAT,
			},
			URL:           target.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", target.Branch)),
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

func getPathOrTree(directory, subDir, method string, target *api.Target) (string, *object.Tree, error) {
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
