package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/redhat-et/harpoon/pkg/engine/utils"

	"k8s.io/klog/v2"
)

const (
	harpoonService = "harpoon"
	defaultVolume  = "harpoon-volume"
	harpoonImage   = "quay.io/harpoon/harpoon:latest"
	systemdImage   = "quay.io/harpoon/harpoon-systemd-amd:latest"

	configMethod       = "config"
	rawMethod          = "raw"
	systemdMethod      = "systemd"
	kubeMethod         = "kube"
	fileTransferMethod = "filetransfer"
	ansibleMethod      = "ansible"
	cleanMethod        = "clean"
	deleteFile         = "delete"
	systemdPathRoot    = "/etc/systemd/system"
	podmanServicePath  = "/usr/lib/systemd/system"
)

var (
	defaultConfigPath   = filepath.Join("/opt", "mount", "config.yaml")
	defaultConfigBackup = filepath.Join("/opt", "mount", "config-backup.yaml")
)

// HarpoonConfig requires necessary objects to process targets
type HarpoonConfig struct {
	Targets []*Target `mapstructure:"targets"`
	PAT     string    `mapstructure:"pat"`

	volume string `mapstructure:"volume"`
	// conn holds podman client
	conn           context.Context
	scheduler      *gocron.Scheduler
	configFile     string
	restartHarpoon bool
}

func NewHarpoonConfig() *HarpoonConfig {
	return &HarpoonConfig{
		Targets: []*Target{
			{
				methodSchedules: make(map[string]string),
			},
		},
	}
}

type SingleMethodObj struct {
	// Conn holds the podman client
	Conn   context.Context
	Method string
	Target *Target
}

var harpoonConfig *HarpoonConfig
var harpoonVolume string

// harpoonCmd represents the base command when called without any subcommands
var harpoonCmd = &cobra.Command{
	Version: "0.0.0",
	Use:     harpoonService,
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
	flags.StringVar(&o.configFile, "config", defaultConfigPath, "file that holds harpoon configuration")
	flags.StringVar(&o.volume, "volume", defaultVolume, "podman volume to hold harpoon data. If volume doesn't exist, harpoon will create it.")
}

// restart fetches new targets from an updated config
// new targets will be added, stale removed, and existing
// will set last commit as last known.
func (hc *HarpoonConfig) Restart() {
	hc.scheduler.RemoveByTags(kubeMethod, ansibleMethod, fileTransferMethod, systemdMethod, rawMethod)
	hc.scheduler.Clear()
	hc.InitConfig(false)
	hc.GetTargets()
	hc.RunTargets()
}

func populateConfig(v *viper.Viper) (*HarpoonConfig, bool, error) {
	config := NewHarpoonConfig()
	flagConfigDir := filepath.Dir(defaultConfigPath)
	flagConfigName := filepath.Base(defaultConfigPath)
	v.AddConfigPath(flagConfigDir)
	v.SetConfigName(flagConfigName)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err == nil {
		if err := v.Unmarshal(&config); err != nil {
			klog.Info("Error with unmarshal of existing config file: %v", err)
			return nil, false, err
		}
	}
	return config, true, nil
}

// This location will be checked first. This is from a `-v /path/to/config.yaml:/opt/mount/config.yaml`,
// If not initial, this may be overwritten with what is currently in HARPOON_CONFIG_URL
func isLocalConfig(v *viper.Viper) (*HarpoonConfig, bool, error) {
	if _, err := os.Stat(defaultConfigPath); err != nil {
		klog.Infof("Local config file not found: %v", err)
		return nil, false, err
	}
	return populateConfig(v)
}

// Initconfig reads in config file and env variables if set.
func (hc *HarpoonConfig) InitConfig(initial bool) {
	v := viper.New()
	var err error
	var isLocal, exists bool
	var config *HarpoonConfig
	envURL := os.Getenv("HARPOON_CONFIG_URL")

	// user will pass path on local system, but it must be mounted at the defaultConfigPath in harpoon pod
	// regardless of where the config file is on the host, harpoon will read the configFile from within
	// the pod at /opt/mount/harpoon-config.yaml
	if initial && hc.configFile != defaultConfigPath {
		if _, err := os.Stat(defaultConfigPath); err != nil {
			cobra.CheckErr(fmt.Errorf("the local config file must be mounted to /opt/mount directory at /opt/mount/config.yaml in the harpoon pod: %v", err))
		}
	}

	config, isLocal, err = isLocalConfig(v)
	if (initial && !isLocal) || err != nil {
		// Only run this from initial startup and only after trying to populate the config from a local file.
		// because CheckForConfigUpdates also runs with each processConfig, so if !initial this is already done
		// If configURL is passed in, a config file on disk has priority on the initial run.
		_ = hc.CheckForConfigUpdates(envURL, false, true)
	}

	// if config is not yet populated, hc.CheckForConfigUpdates has placed the config
	// downloaded from URL to the defaultconfigPath
	if !isLocal {
		// If not initial run, only way to get here is if already determined need for reload
		// with an updated config placed in defaultConfigPath.
		config, exists, err = populateConfig(v)
		if config == nil || !exists || err != nil {
			if err != nil {
				cobra.CheckErr(fmt.Errorf("Could not populate config, tried local %s in harpoon pod and also URL: %s", defaultConfigPath, envURL))
			}
			cobra.CheckErr(fmt.Errorf("Error locating config, tried local %s in harpoon pod and also URL %s: %v", defaultConfigPath, envURL, err))
		}
	}

	if config == nil || config.Targets == nil {
		cobra.CheckErr("no harpoon targets found, exiting")
	}

	if config.volume == "" {
		config.volume = defaultVolume
	}

	harpoonVolume = config.volume
	ctx := context.Background()
	if hc.conn == nil {
		// TODO: socket directory same for all platforms?
		// sock_dir := os.Getenv("XDG_RUNTIME_DIR")
		// socket := "unix:" + sock_dir + "/podman/podman.sock"
		conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
		if err != nil || conn == nil {
			cobra.CheckErr(fmt.Errorf("error establishing connection to podman.sock: %v", err))
		}
		hc.conn = conn
	}

	if err := detectOrFetchImage(hc.conn, harpoonImage, false); err != nil {
		cobra.CheckErr(err)
	}

	beforeTargets := len(hc.Targets)
	hc.Targets = config.Targets
	if beforeTargets > 0 {
		// replace lastCommit - to avoid re-running same jobs, since the scheduler finished all jobs
		// with the last commit before arriving here
		for i, t := range hc.Targets {
			if t.Methods.Raw != nil {
				if config.Targets[i].Methods.Raw != nil {
					t.Methods.Raw.lastCommit = config.Targets[i].Methods.Raw.lastCommit
				}
			}
			if t.Methods.Kube != nil {
				if config.Targets[i].Methods.Kube != nil {
					t.Methods.Kube.lastCommit = config.Targets[i].Methods.Kube.lastCommit
				}
			}
			if t.Methods.Ansible != nil {
				if config.Targets[i].Methods.Ansible != nil {
					t.Methods.Ansible.lastCommit = config.Targets[i].Methods.Ansible.lastCommit
				}
			}
			if t.Methods.FileTransfer != nil {
				if config.Targets[i].Methods.FileTransfer != nil {
					t.Methods.FileTransfer.lastCommit = config.Targets[i].Methods.FileTransfer.lastCommit
				}
			}
			if t.Methods.Systemd != nil {
				if config.Targets[i].Methods.Systemd != nil {
					t.Methods.Systemd.lastCommit = config.Targets[i].Methods.Systemd.lastCommit
				}
			}
		}
	}

	// look for a ConfigFileTarget, only find the first
	// TODO: add logic to merge multiple configs
	for _, t := range hc.Targets {
		if t.Methods.ConfigTarget == nil {
			continue
		}
		// reset URL if necessary
		// ConfigUrl set in config file overrides env variable
		// If the same, this is no change, if diff then the new config has updated the configUrl
		if t.Methods.ConfigTarget.ConfigUrl != "" {
			os.Setenv("HARPOON_CONFIG_URL", t.Methods.ConfigTarget.ConfigUrl)
		}
		break
	}

	if hc.scheduler == nil {
		hc.scheduler = gocron.NewScheduler(time.UTC)
	}
}

// GetTargets returns map of repoName to map of method:Schedule
func (hc *HarpoonConfig) GetTargets() {
	for _, target := range hc.Targets {
		target.mu.Lock()
		defer target.mu.Unlock()
		schedMethods := make(map[string]string)
		if target.Methods.ConfigTarget != nil {
			schedMethods[configMethod] = target.Methods.ConfigTarget.Schedule
		}
		if target.Methods.Raw != nil {
			target.Methods.Raw.initialRun = true
			schedMethods[rawMethod] = target.Methods.Raw.Schedule
		}
		if target.Methods.Kube != nil {
			target.Methods.Kube.initialRun = true
			schedMethods[kubeMethod] = target.Methods.Kube.Schedule
		}
		if target.Methods.Systemd != nil {
			target.Methods.Systemd.initialRun = true
			schedMethods[systemdMethod] = target.Methods.Systemd.Schedule
			// podman auto-update service is enabled on initialRun regardless of schedule
			if target.Methods.Systemd.Schedule == "" && target.Methods.Systemd.AutoUpdateAll {
				schedMethods[systemdMethod] = "*/1 * * * *"
			}
		}
		if target.Methods.FileTransfer != nil {
			target.Methods.FileTransfer.initialRun = true
			schedMethods[fileTransferMethod] = target.Methods.FileTransfer.Schedule
		}
		if target.Methods.Ansible != nil {
			target.Methods.Ansible.initialRun = true
			schedMethods[ansibleMethod] = target.Methods.Ansible.Schedule
		}
		if target.Methods.Clean != nil {
			schedMethods[cleanMethod] = target.Methods.Clean.Schedule
		}
		target.methodSchedules = schedMethods
		hc.update(target)
	}
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (hc *HarpoonConfig) RunTargets() {
	allTargets := make(map[string]map[string]string)
	for _, target := range hc.Targets {
		if target.Url != "" {
			if err := hc.getClone(target); err != nil {
				klog.Warningf("Target: %s, clone error: %v, will retry next scheduled run", target.Name, err)
			}
		}
		allTargets[target.Name] = target.methodSchedules
	}

	s := hc.scheduler
	for repoName, schedMethods := range allTargets {
		var target Target
		for _, t := range hc.Targets {
			if repoName == t.Name {
				target = *t
			}
		}

		for method, schedule := range schedMethods {
			switch method {
			case configMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(configMethod).Do(hc.processConfig, ctx, &target)
			case kubeMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(kubeMethod).Do(hc.processKube, ctx, &target)
			case rawMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(rawMethod).Do(hc.processRaw, ctx, &target)
			case systemdMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(systemdMethod).Do(hc.processSystemd, ctx, &target)
			case fileTransferMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(fileTransferMethod).Do(hc.processFileTransfer, ctx, &target)
			case ansibleMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(ansibleMethod).Do(hc.processAnsible, ctx, &target)
			case cleanMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule).Tag(cleanMethod).Do(hc.processClean, ctx, &target)
			default:
				klog.Warningf("Target: %s Method: %s, unknown method type, ignoring", target.Name, method)
			}
		}
	}
	s.StartImmediately()
	s.StartAsync()
	select {}
}

func (hc *HarpoonConfig) processConfig(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	// configUrl in config file will override the environment variable
	config := target.Methods.ConfigTarget
	envURL := os.Getenv("HARPOON_CONFIG_URL")
	// config.Url from target overrides env variable
	if config.ConfigUrl != "" {
		envURL = config.ConfigUrl
	}
	os.Setenv("HARPOON_CONFIG_URL", envURL)
	// If ConfigUrl is not populated, warn and leave
	if envURL == "" {
		klog.Warningf("Harpoon ConfigFileTarget found, but neither $HARPOON_CONFIG_URL on system nor ConfigTarget.ConfigUrl are set, exiting without updating the config.")
	}
	// CheckForConfigUpdates downloads & places config file in defaultConfigPath
	// if the downloaded config file differs from what's currently on the system.
	restart := hc.CheckForConfigUpdates(envURL, true, false)
	if !restart {
		return
	}
	hc.restartHarpoon = restart
	hc.update(target)
	if hc.restartHarpoon {
		klog.Info("Updated config processed, restarting with new targets")
		harpoonConfig.Restart()
	}
}

func (hc *HarpoonConfig) processRaw(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	raw := target.Methods.Raw
	initial := raw.initialRun
	tag := []string{".json", ".yaml", ".yml"}
	var targetFile string
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: rawMethod,
		Target: target,
	}
	var path string
	if initial {
		retry := hc.resetTarget(target, rawMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, err := hc.getPathOrTree(target, raw.TargetPath, rawMethod)
		if err != nil {
			_ = hc.resetTarget(target, rawMethod, false, err)
			return
		}
		targetFile, err = hc.applyInitial(ctx, mo, fileName, raw.TargetPath, &tag, subDirTree)
		if err != nil {
			_ = hc.resetTarget(target, rawMethod, false, err)
			return
		}
		path = targetFile
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		raw.initialRun = hc.resetTarget(target, rawMethod, false, err)
		return
	}
	raw.initialRun = false
	hc.update(target)
}

func (hc *HarpoonConfig) processAnsible(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	ans := target.Methods.Ansible
	initial := ans.initialRun
	tag := []string{"yaml", "yml"}
	var targetFile = ""
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: ansibleMethod,
		Target: target,
	}
	var path string
	if initial {
		retry := hc.resetTarget(target, ansibleMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, err := hc.getPathOrTree(target, ans.TargetPath, ansibleMethod)
		if err != nil {
			_ = hc.resetTarget(target, ansibleMethod, false, err)
			return
		}
		targetFile, err = hc.applyInitial(ctx, mo, fileName, ans.TargetPath, &tag, subDirTree)
		if err != nil {
			_ = hc.resetTarget(target, ansibleMethod, false, err)
			return
		}
		path = targetFile
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		ans.initialRun = hc.resetTarget(target, ansibleMethod, false, err)
		return
	}
	ans.initialRun = false
	hc.update(target)
}

func (hc *HarpoonConfig) processSystemd(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	sd := target.Methods.Systemd
	if sd.AutoUpdateAll && !sd.initialRun {
		return
	}
	if sd.AutoUpdateAll {
		sd.Enable = false
		target.Url = ""
		sd.Root = true
		sd.TargetPath = ""
		sd.Restart = false
	}
	initial := sd.initialRun
	var targetFile string
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: systemdMethod,
		Target: target,
	}
	tag := []string{".service"}
	if sd.Restart {
		sd.Enable = true
	}
	var path string
	if initial {
		if sd.AutoUpdateAll {
			if err := hc.EngineMethod(ctx, mo, "", nil); err != nil {
				klog.Infof("Failed to start podman-auto-update.service: %v", err)
			}
			sd.initialRun = false
			hc.update(target)
			return
		}
		retry := hc.resetTarget(target, systemdMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, err := hc.getPathOrTree(target, sd.TargetPath, systemdMethod)
		if err != nil {
			_ = hc.resetTarget(target, systemdMethod, false, err)
			return
		}
		targetFile, err = hc.applyInitial(ctx, mo, fileName, sd.TargetPath, &tag, subDirTree)
		if err != nil {
			_ = hc.resetTarget(target, systemdMethod, false, err)
			return
		}
		path = targetFile
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		sd.initialRun = hc.resetTarget(target, systemdMethod, false, err)
		return
	}
	sd.initialRun = false
	hc.update(target)
}

func (hc *HarpoonConfig) processFileTransfer(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	ft := target.Methods.FileTransfer
	initial := ft.initialRun
	var targetFile string
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: fileTransferMethod,
		Target: target,
	}
	var path string
	if initial {
		retry := hc.resetTarget(target, fileTransferMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, err := hc.getPathOrTree(target, ft.TargetPath, fileTransferMethod)
		if err != nil {
			_ = hc.resetTarget(target, fileTransferMethod, false, err)
			return
		}
		targetFile, err = hc.applyInitial(ctx, mo, fileName, ft.TargetPath, nil, subDirTree)
		if err != nil {
			_ = hc.resetTarget(target, fileTransferMethod, false, err)
			return
		}
		path = targetFile
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		ft.initialRun = hc.resetTarget(target, fileTransferMethod, false, err)
		return
	}
	ft.initialRun = false
	hc.update(target)
}

func (hc *HarpoonConfig) processKube(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()

	kube := target.Methods.Kube
	initial := kube.initialRun
	tag := []string{"yaml", "yml"}
	var targetFile string
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: kubeMethod,
		Target: target,
	}
	var path string
	if initial {
		retry := hc.resetTarget(target, kubeMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, err := hc.getPathOrTree(target, kube.TargetPath, kubeMethod)
		if err != nil {
			_ = hc.resetTarget(target, kubeMethod, false, err)
			return
		}
		targetFile, err = hc.applyInitial(ctx, mo, fileName, kube.TargetPath, &tag, subDirTree)
		if err != nil {
			_ = hc.resetTarget(target, kubeMethod, false, err)
			return
		}
		path = targetFile
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		kube.initialRun = hc.resetTarget(target, kubeMethod, false, err)
		return
	}
	kube.initialRun = false
	hc.update(target)
}

func (hc *HarpoonConfig) processClean(ctx context.Context, target *Target) {
	target.mu.Lock()
	defer target.mu.Unlock()
	// Nothing to do with certain file we're just collecting garbage so can call the cleanPodman method straight from here
	opts := system.PruneOptions{
		All:     &target.Methods.Clean.All,
		Volumes: &target.Methods.Clean.Volumes,
	}

	err := cleanPodman(ctx, hc.conn, opts)
	if err != nil {
		klog.Warningf("Repo: %s Method: %s encountered error: %v, resetting...", target.Name, cleanMethod, err)
	}

	hc.update(target)
}

func (hc *HarpoonConfig) applyInitial(ctx context.Context, mo *SingleMethodObj, fileName, tp string, tag *[]string, subDirTree *object.Tree) (string, error) {
	directory := filepath.Base(mo.Target.Url)
	if fileName != "" {
		found := false
		if checkTag(tag, fileName) {
			found = true
			path := filepath.Join(directory, fileName)
			if err := hc.EngineMethod(ctx, mo, path, nil); err != nil {
				return fileName, utils.WrapErr(err, "error running engine with method %s, for file %s",
					mo.Method, fileName)
			}
		}
		if !found {
			err := fmt.Errorf("%s target file must be of type %v", mo.Method, tag)
			return fileName, utils.WrapErr(err, "error running engine with method %s, for file %s",
				mo.Method, fileName)
		}

	} else {
		// ... get the files iterator and print the file
		ch := make(chan error)
		subDirTree.Files().ForEach(func(f *object.File) error {
			go func(ch chan<- error) {
				if checkTag(tag, f.Name) {
					path := filepath.Join(directory, tp, f.Name)
					if err := hc.EngineMethod(ctx, mo, path, nil); err != nil {
						ch <- utils.WrapErr(err, "error running engine with method %s, for file %s",
							mo.Method, path)
					}
				}
				ch <- nil
			}(ch)
			return nil
		})

		err := subDirTree.Files().ForEach(func(_ *object.File) error {
			err := <-ch
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return fileName, err
		}
	}
	return fileName, nil
}

func (hc *HarpoonConfig) getChangesAndRunEngine(ctx context.Context, mo *SingleMethodObj, path string) error {
	var lc *object.Commit
	var targetPath string
	switch mo.Method {
	case rawMethod:
		lc = mo.Target.Methods.Raw.lastCommit
		targetPath = mo.Target.Methods.Raw.TargetPath
	case kubeMethod:
		lc = mo.Target.Methods.Kube.lastCommit
		targetPath = mo.Target.Methods.Kube.TargetPath
	case ansibleMethod:
		lc = mo.Target.Methods.Ansible.lastCommit
		targetPath = mo.Target.Methods.Ansible.TargetPath
	case fileTransferMethod:
		lc = mo.Target.Methods.FileTransfer.lastCommit
		targetPath = mo.Target.Methods.FileTransfer.TargetPath
	case systemdMethod:
		lc = mo.Target.Methods.Systemd.lastCommit
		targetPath = mo.Target.Methods.Systemd.TargetPath
	default:
		return fmt.Errorf("unknown method: %s", mo.Method)
	}
	tp := targetPath
	if path != "" {
		tp = path
	}
	changesThisMethod, newCommit, err := hc.findDiff(mo.Target, mo.Method, tp, lc)
	if err != nil {
		return utils.WrapErr(err, "error method: %s commit: %s", mo.Method, lc.Hash.String())
	}

	hc.setlastCommit(mo.Target, mo.Method, newCommit)
	hc.update(mo.Target)

	if len(changesThisMethod) == 0 {
		if mo.Method == systemdMethod && mo.Target.Methods.Systemd.Restart && !mo.Target.Methods.Systemd.initialRun {
			return hc.EngineMethod(ctx, mo, filepath.Base(mo.Target.Methods.Systemd.TargetPath), nil)
		}
		klog.Infof("Target: %s, Method: %s: Nothing to pull.....Requeuing", mo.Target.Name, mo.Method)
		return nil
	}

	ch := make(chan error)
	for change, changePath := range changesThisMethod {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := hc.EngineMethod(ctx, mo, changePath, change); err != nil {
				ch <- utils.WrapErr(err, "error method: %s path: %s, commit: %s", mo.Method, changePath, newCommit.Hash.String())
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changesThisMethod {
		err := <-ch
		if err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (hc *HarpoonConfig) update(target *Target) {
	for _, t := range hc.Targets {
		if target.Name == t.Name {
			t = target
		}
	}
}

func (hc *HarpoonConfig) findDiff(target *Target, method, targetPath string, commit *object.Commit) (map[*object.Change]string, *object.Commit, error) {
	directory := filepath.Base(target.Url)
	// map of change to path
	thisMethodChanges := make(map[*object.Change]string)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		return thisMethodChanges, nil, fmt.Errorf("error while opening the repository: %v", err)
	}
	w, err := gitRepo.Worktree()
	if err != nil {
		return thisMethodChanges, nil, fmt.Errorf("error while opening the worktree: %v", err)
	}
	// ... retrieve the tree from this method's last fetched commit
	beforeFetchTree, _, err := getTree(gitRepo, commit)
	if err != nil {
		// TODO: if lastCommit has disappeared, need to reset and set initial=true instead of exit
		return thisMethodChanges, nil, fmt.Errorf("error checking out last known commit, has branch been force-pushed, commit no longer exists?: %v", err)
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
		return thisMethodChanges, nil, fmt.Errorf("error checking out latest branch %s: %v", ref, err)
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

// Each engineMethod call now owns the prev and dest variables instead of being shared in mo
func (hc *HarpoonConfig) EngineMethod(ctx context.Context, mo *SingleMethodObj, path string, change *object.Change) error {
	switch mo.Method {
	case rawMethod:
		prev, err := getChangeString(change)
		if err != nil {
			return err
		}
		return rawPodman(ctx, mo, path, prev)
	case systemdMethod:
		// TODO: add logic for non-root services
		var prev *string = nil
		if change != nil {
			if change.To.Name != "" {
				prev = &change.To.Name
			}
		}
		nonRootHomeDir := os.Getenv("HOME")
		if nonRootHomeDir == "" {
			return fmt.Errorf("Could not determine $HOME for host, must set $HOME on host machine for non-root systemd method")
		}
		var dest string
		if mo.Target.Methods.Systemd.Root {
			dest = systemdPathRoot
		} else {
			dest = filepath.Join(nonRootHomeDir, ".config", "systemd", "user")
		}
		if change != nil {
			mo.Target.Methods.Systemd.initialRun = true
		}
		return systemdPodman(ctx, mo, path, dest, prev)
	case fileTransferMethod:
		var prev *string = nil
		if change != nil {
			if change.To.Name != "" {
				prev = &change.To.Name
			}
		}
		dest := mo.Target.Methods.FileTransfer.DestinationDirectory
		return fileTransferPodman(ctx, mo, path, dest, prev)
	case kubeMethod:
		prev, err := getChangeString(change)
		if err != nil {
			return err
		}
		return kubePodman(ctx, mo, path, prev)
	case ansibleMethod:
		return ansiblePodman(ctx, mo, path)
	default:
		return fmt.Errorf("unsupported method: %s", mo.Method)
	}
}

func (hc *HarpoonConfig) getClone(target *Target) error {
	directory := filepath.Base(target.Url)
	absPath, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	var exists bool
	if _, err := os.Stat(directory); err == nil {
		exists = true
		// if directory/.git does not exist, fail quickly
		if _, err := os.Stat(directory + "/.git"); err != nil {
			return fmt.Errorf("%s exists but is not a git repository", directory)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if !exists {
		klog.Infof("git clone %s %s --recursive", target.Url, target.Branch)
		var user string
		if hc.PAT != "" {
			user = "harpoon"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user, // the value of this field should not matter when using a PAT
				Password: hc.PAT,
			},
			URL:           target.Url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", target.Branch)),
			SingleBranch:  true,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (hc *HarpoonConfig) getPathOrTree(target *Target, subDir, method string) (string, *object.Tree, error) {
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		return "", nil, err
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

// arrive at resetTarget 1 of 2 ways:
//      1) initial run of target - will return true if clone or commit fetch fails, to try again next run
//      2) processing error during run - will attempt to fetch the remote commit and reset to initialRun true for the
//         next run, or, if fetching of commit fails, will return true to try again next run
// resetTarget returns true if the target should be re-cloned next run, and it will set
func (hc *HarpoonConfig) resetTarget(target *Target, method string, initial bool, err error) bool {
	if err != nil {
		klog.Warningf("Target: %s Method: %s encountered error: %v, resetting...", target.Name, method, err)
	}
	commit, err := hc.getGit(target, initial)
	if err != nil {
		klog.Warningf("Target: %s error getting next commit, will try again next scheduled run: %v", target.Name, err)
		return true
	}
	if commit == nil {
		klog.Warningf("Target: %s, fetched empty commit, will retry next scheduled run", target.Name)
		return true
	}

	return hc.setInitial(target, commit, method)
}

func (hc *HarpoonConfig) getGit(target *Target, initialRun bool) (*object.Commit, error) {
	if initialRun {
		if err := hc.getClone(target); err != nil {
			return nil, err
		}
	}
	directory := filepath.Base(target.Url)
	gitRepo, err := git.PlainOpen(directory)
	if err != nil {
		return nil, err
	}

	_, commit, err := getTree(gitRepo, nil)
	if err != nil {
		return nil, err
	}
	return commit, nil
}

// setInitial will return true if fetching of commit fails or results in empty commit, to try again next run
// or, if valid commit is fetched, will set initialRun true and lastCommit for the method, to process next run
func (hc *HarpoonConfig) setInitial(target *Target, commit *object.Commit, method string) bool {
	retry := false
	hc.setinitialRun(target, method)
	if commit == nil {
		retry = true
	} else {
		hc.setlastCommit(target, method, commit)
	}
	hc.update(target)
	return retry
}

func (hc *HarpoonConfig) setlastCommit(target *Target, method string, commit *object.Commit) {
	switch method {
	case kubeMethod:
		target.Methods.Kube.lastCommit = commit
	case rawMethod:
		target.Methods.Raw.lastCommit = commit
	case systemdMethod:
		target.Methods.Systemd.lastCommit = commit
	case fileTransferMethod:
		target.Methods.FileTransfer.lastCommit = commit
	case ansibleMethod:
		target.Methods.Ansible.lastCommit = commit
	}
}

// setinitialRun is called before the initial processing of a target, or
// upon any processing errors for the method, so the method will be retried with next run
func (hc *HarpoonConfig) setinitialRun(target *Target, method string) {
	switch method {
	case kubeMethod:
		target.Methods.Kube.initialRun = true
	case rawMethod:
		target.Methods.Raw.initialRun = true
	case systemdMethod:
		target.Methods.Systemd.initialRun = true
	case fileTransferMethod:
		target.Methods.FileTransfer.initialRun = true
	case ansibleMethod:
		target.Methods.Ansible.initialRun = true
	}
}

// CheckForConfigUpdates, downloads, & places config file
// in defaultConfigPath in harpoon container (/opt/mount/config.yaml).
// This runs with the initial startup as well as with scheduled ConfigTarget runs,
// if $HARPOON_CONFIG_URL is set.
func (hc *HarpoonConfig) CheckForConfigUpdates(envURL string, existsAlready bool, initial bool) bool {
	// envURL is either set by user or set to match a configUrl in a configTarget
	if envURL == "" {
		return false
	}
	reset, err := downloadUpdateConfigFile(envURL, existsAlready, initial)
	if err != nil {
		klog.Info(err)
	}
	return reset
}
