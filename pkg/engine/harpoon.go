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
	"github.com/go-git/go-git/v5/config"
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
	deleteFile         = "delete"
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
		if err := hc.getClone(target); err != nil {
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
		directory := filepath.Base(target.Url)
		gitRepo, err := git.PlainOpen(directory)
		if err != nil {
			log.Fatalf("Repo: %s, error while opening the repository: %v", directory, err)
		}
		_, commit, err := getTree(gitRepo, nil)
		if err != nil {
			log.Fatalf("Repo: %s, error getting repository tree: %v", directory, err)
		}

		for method, schedule := range schedMethods {
			switch method {
			case kubeMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Mu.Lock()
				target.Kube.InitialRun = true
				target.Kube.LastCommit = commit
				hc.update(&target)
				target.Mu.Unlock()
				s.Cron(schedule).Do(hc.processKube, ctx, &target, schedule)
			case rawMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Mu.Lock()
				target.Raw.InitialRun = true
				target.Raw.LastCommit = commit
				hc.update(&target)
				target.Mu.Unlock()
				s.Cron(schedule).Do(hc.processRaw, ctx, &target, schedule)
			case systemdMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Mu.Lock()
				target.Systemd.InitialRun = true
				target.Systemd.LastCommit = commit
				hc.update(&target)
				target.Mu.Unlock()
				s.Cron(schedule).Do(hc.processSystemd, ctx, &target, schedule)
			case fileTransferMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Mu.Lock()
				target.FileTransfer.InitialRun = true
				target.FileTransfer.LastCommit = commit
				hc.update(&target)
				target.Mu.Unlock()
				s.Cron(schedule).Do(hc.processFileTransfer, ctx, &target, schedule)
			case ansibleMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				target.Mu.Lock()
				target.Ansible.InitialRun = true
				target.Ansible.LastCommit = commit
				hc.update(&target)
				target.Mu.Unlock()
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
	hc.update(target)
	tag := []string{".json"}
	var targetFile string
	if initial {
		fileName, subDirTree, err := hc.getPathOrTree(target, target.Raw.TargetPath, rawMethod)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, target.Raw.TargetPath, rawMethod, &tag, subDirTree, target)
		if err != nil {
			log.Fatalf("Repo: %s Method: %s, error while processing the repository: %s", target.Name, rawMethod, err)
		}
	}

	if err := hc.getChangesAndRunEngine(ctx, rawMethod, targetFile, target); err != nil {
		log.Fatalf("Repo: %s Method: %s, error while processing repository changes: %v", target.Name, rawMethod, err)
	}
}

func (hc *HarpoonConfig) processAnsible(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Ansible.InitialRun
	target.Ansible.InitialRun = false
	hc.update(target)
	tag := []string{"yaml", "yml"}
	var targetFile = ""
	if initial {
		fileName, subDirTree, err := hc.getPathOrTree(target, target.Ansible.TargetPath, ansibleMethod)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, target.Ansible.TargetPath, ansibleMethod, &tag, subDirTree, target)
		if err != nil {
			log.Fatal(err)
		}
	}

	if err := hc.getChangesAndRunEngine(ctx, ansibleMethod, targetFile, target); err != nil {
		log.Fatalf("Repo: %s Method: %s, error while processing repository changes: %v", target.Name, ansibleMethod, err)
	}
}

func (hc *HarpoonConfig) processSystemd(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Systemd.InitialRun
	target.Systemd.InitialRun = false
	hc.update(target)
	var targetFile string
	tag := []string{".service"}
	if initial {
		fileName, subDirTree, err := hc.getPathOrTree(target, target.Systemd.TargetPath, systemdMethod)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, target.Systemd.TargetPath, systemdMethod, &tag, subDirTree, target)
		if err != nil {
			log.Fatalf("Repo: %s Method: %s, error while processing the repository: %s", target.Name, systemdMethod, err)
		}
	}

	if err := hc.getChangesAndRunEngine(ctx, systemdMethod, targetFile, target); err != nil {
		log.Fatalf("Repo: %s Method: %s, error while processing repository changes: %v", target.Name, systemdMethod, err)
	}
}

func (hc *HarpoonConfig) processFileTransfer(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.FileTransfer.InitialRun
	target.FileTransfer.InitialRun = false
	hc.update(target)
	var targetFile string
	if initial {
		fileName, subDirTree, err := hc.getPathOrTree(target, target.FileTransfer.TargetPath, fileTransferMethod)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, target.FileTransfer.TargetPath, fileTransferMethod, nil, subDirTree, target)
		if err != nil {
			log.Fatalf("Repo: %s Method: %s, error while processing the repository: %s", target.Name, fileTransferMethod, err)
		}
	}

	if err := hc.getChangesAndRunEngine(ctx, fileTransferMethod, targetFile, target); err != nil {
		log.Fatalf("Repo: %s Method: %s, error while processing repository changes: %v", target.Name, fileTransferMethod, err)
	}
}

func (hc *HarpoonConfig) processKube(ctx context.Context, target *api.Target, schedule string) {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	initial := target.Kube.InitialRun
	target.Kube.InitialRun = false
	hc.update(target)
	tag := []string{"yaml", "yml"}
	var targetFile string
	if initial {
		fileName, subDirTree, err := hc.getPathOrTree(target, target.Kube.TargetPath, kubeMethod)
		if err != nil {
			log.Fatal(err)
		}
		targetFile, err = hc.applyInitial(ctx, fileName, target.Kube.TargetPath, kubeMethod, &tag, subDirTree, target)
		if err != nil {
			log.Fatalf("Repo: %s Method: %s, error while processing the repository: %s", target.Name, kubeMethod, err)
		}
	}

	if err := hc.getChangesAndRunEngine(ctx, kubeMethod, targetFile, target); err != nil {
		log.Fatalf("Repo: %s Method: %s, error while processing repository changes: %v", target.Name, kubeMethod, err)
	}
}

func (hc *HarpoonConfig) applyInitial(ctx context.Context, fileName, tp, method string, tag *[]string, subDirTree *object.Tree, target *api.Target) (string, error) {
	directory := filepath.Base(target.Url)
	if fileName != "" {
		found := false
		if hc.checkTag(tag, fileName) {
			found = true
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, path, method, target, nil); err != nil {
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
				if err := hc.EngineMethod(ctx, path, method, target, nil); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fileName, err
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

func (hc *HarpoonConfig) setLastCommit(target *api.Target, method string, commit *object.Commit) error {
	switch method {
	case ansibleMethod:
		target.Ansible.LastCommit = commit
	case rawMethod:
		target.Raw.LastCommit = commit
	case systemdMethod:
		target.Systemd.LastCommit = commit
	case kubeMethod:
		target.Kube.LastCommit = commit
	case fileTransferMethod:
		target.FileTransfer.LastCommit = commit
	default:
		return fmt.Errorf("unknown method: %s", method)
	}
	hc.update(target)
	return nil
}

func (hc *HarpoonConfig) getChangesAndRunEngine(ctx context.Context, method, targetFile string, target *api.Target) error {
	var lastCommit *object.Commit
	var targetPath string
	switch method {
	case rawMethod:
		lastCommit = target.Raw.LastCommit
		targetPath = target.Raw.TargetPath
	case kubeMethod:
		lastCommit = target.Kube.LastCommit
		targetPath = target.Kube.TargetPath
	case ansibleMethod:
		lastCommit = target.Ansible.LastCommit
		targetPath = target.Ansible.TargetPath
	case fileTransferMethod:
		lastCommit = target.FileTransfer.LastCommit
		targetPath = target.FileTransfer.TargetPath
	case systemdMethod:
		lastCommit = target.Systemd.LastCommit
		targetPath = target.Systemd.TargetPath
	default:
		return fmt.Errorf("unknown method: %s", method)
	}
	tp := targetPath
	if targetFile != "" {
		tp = targetFile
	}
	changesThisMethod, newCommit, err := hc.findDiff(target, method, tp, lastCommit)
	if err != nil {
		return err
	}
	hc.setLastCommit(target, method, newCommit)
	if len(changesThisMethod) == 0 {
		klog.Infof("Repo: %s, Method: %s: Nothing to pull.....Requeuing", target.Name, method)
		return nil
	}

	for change, path := range changesThisMethod {
		if err := hc.EngineMethod(ctx, path, method, target, change); err != nil {
			return err
		}
	}
	return nil
}

func (hc *HarpoonConfig) update(target *api.Target) {
	for _, t := range hc.Targets {
		if target.Name == t.Name {
			t = target
		}
	}
}

func (hc *HarpoonConfig) findDiff(target *api.Target, method, targetPath string, commit *object.Commit) (map[*object.Change]string, *object.Commit, error) {
	directory := filepath.Base(target.Url)
	// map of change to path
	thisMethodChanges := make(map[*object.Change]string)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		return thisMethodChanges, nil, fmt.Errorf("Repo: %s, Method: %s, error while opening the repository: %v", directory, method, err)
	}
	w, err := gitRepo.Worktree()
	if err != nil {
		return thisMethodChanges, nil, fmt.Errorf("Repo: %s Method: %s, error while opening the worktree: %s", directory, method, err)
	}
	// ... retrieve the tree from this method's last fetched commit
	beforeFetchTree, _, err := getTree(gitRepo, commit)
	if err != nil {
		// TODO: if LastCommit has disappeared, need to reset and set initial=true instead of exit
		return thisMethodChanges, nil, fmt.Errorf("Repo: %s Method: %s, error checking out last known commit, has branch been force-pushed, commit no longer exists?: %v", directory, method, err)
	}

	// Fetch the latest changes from the origin remote and merge into the current branch
	ref := fmt.Sprintf("refs/heads/%s", target.Branch)
	refName := plumbing.ReferenceName(ref)
	refSpec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", target.Branch, target.Branch))
	if err = gitRepo.Fetch(&git.FetchOptions{
		RefSpecs: []config.RefSpec{refSpec, "HEAD:refs/heads/HEAD"},
		Force:    true,
	}); err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, commit, err
	}

	// force checkout to latest fetched branch
	if err := w.Checkout(&git.CheckoutOptions{
		Branch: refName,
		Force:  true,
	}); err != nil {
		return thisMethodChanges, nil, fmt.Errorf("Repo: %s, error checking out latest branch %s: %v", directory, ref, err)
	}

	afterFetchTree, newestCommit, err := getTree(gitRepo, nil)
	if err != nil {
		return thisMethodChanges, nil, err
	}

	changes, err := afterFetchTree.Diff(beforeFetchTree)
	if err != nil {
		return thisMethodChanges, nil, fmt.Errorf("%s: error while generating diff: %s", directory, err)
	}
	// the change logic is backwards "From" is actually "To"
	for _, change := range changes {
		if strings.Contains(change.From.Name, targetPath) {
			path := directory + "/" + change.From.Name
			thisMethodChanges[change] = path
		} else if strings.Contains(change.To.Name, targetPath) {
			thisMethodChanges[change] = deleteFile
		}

	}
	return thisMethodChanges, newestCommit, nil
}

func (hc *HarpoonConfig) EngineMethod(ctx context.Context, path, method string, target *api.Target, change *object.Change) error {
	switch method {
	case rawMethod:
		var prev *string = getChangeString(change)
		return rawPodman(ctx, path, target.Raw.PullImage, prev)
	case systemdMethod:
		// TODO: add logic for non-root services
		var prev *string = nil
		if change != nil {
			if change.To.Name != "" {
				prev = &change.To.Name
			}
		}
		dest := "/etc/systemd/system"
		return systemdPodman(ctx, path, dest, target, prev)
	case fileTransferMethod:
		var prev *string = nil
		if change != nil {
			if change.To.Name != "" {
				prev = &change.To.Name
			}
		}
		dest := target.FileTransfer.DestinationDirectory
		return fileTransferPodman(ctx, path, dest, fileTransferMethod, target, prev)
	case kubeMethod:
		var prev *string = getChangeString(change)
		return kubePodman(ctx, path, prev)
	case ansibleMethod:
		return ansiblePodman(ctx, path, target.Name, target.Ansible.SshDirectory)
	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
}

func getChangeString(change *object.Change) *string {
	if change != nil {
		_, to, err := change.Files()
		if err != nil {
			log.Fatal(err)
		}
		if to != nil {
			s, err := to.Contents()
			if err != nil {
				log.Fatal(err)
			}
			return &s
		}
	}
	return nil
}

// This assumes unique urls - only 1 git repo per "directory"
func (hc *HarpoonConfig) getClone(target *api.Target) error {
	target.Mu.Lock()
	defer target.Mu.Unlock()
	directory := filepath.Base(target.Url)
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

func getTree(r *git.Repository, oldCommit *object.Commit) (*object.Tree, *object.Commit, error) {
	if oldCommit != nil {
		// ... retrieve the tree from the commit
		tree, err := oldCommit.Tree()
		if err != nil {
			return nil, nil, fmt.Errorf("error when retrieving tree: %s", err)
		}
		return tree, nil, nil
	}
	var newCommit *object.Commit
	ref, err := r.Head()
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving head: %s", err)
	}
	// ... retrieving the commit object
	newCommit, err = r.CommitObject(ref.Hash())
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving commit: %s", err)
	}

	// ... retrieve the tree from the commit
	tree, err := newCommit.Tree()
	if err != nil {
		return nil, nil, fmt.Errorf("error when retrieving tree: %s", err)
	}
	return tree, newCommit, nil
}

func (hc *HarpoonConfig) getPathOrTree(target *api.Target, subDir, method string) (string, *object.Tree, error) {
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		log.Fatalf("Repo: %s, error while opening the repository: %s", directory, err)
	}
	tree, _, err := getTree(gitRepo, nil)
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
