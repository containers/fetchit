package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/redhat-et/fetchit/pkg/engine/utils"

	"k8s.io/klog/v2"
)

const (
	fetchitService = "fetchit"
	defaultVolume  = "fetchit-volume"
	fetchitImage   = "quay.io/fetchit/fetchit:latest"
	systemdImage   = "quay.io/fetchit/fetchit-systemd-amd:latest"

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

// FetchitConfig requires necessary objects to process targets
type FetchitConfig struct {
	Targets []*Target `mapstructure:"targets"`
	PAT     string    `mapstructure:"pat"`

	volume string `mapstructure:"volume"`
	// conn holds podman client
	conn           context.Context
	scheduler      *gocron.Scheduler
	configFile     string
	restartFetchit bool
	gm             *GitManager
}

func NewFetchitConfig() *FetchitConfig {
	return &FetchitConfig{
		Targets: []*Target{
			{
				methodSchedules: make(map[string]schedInfo),
			},
		},
		gm: newGitManager(),
	}
}

type SingleMethodObj struct {
	// Conn holds the podman client
	Conn   context.Context
	Method string
	Target *Target
}

var fetchitConfig *FetchitConfig
var fetchitVolume string

// fetchitCmd represents the base command when called without any subcommands
var fetchitCmd = &cobra.Command{
	Version: "0.0.0",
	Use:     fetchitService,
	Short:   "a tool to schedule gitOps workflows",
	Long:    "Fetchit is a tool to schedule gitOps workflows based on a given configuration file",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags
// appropriately. This is called by main.main().
func Execute() {
	cobra.CheckErr(fetchitCmd.Execute())
}

func (o *FetchitConfig) bindFlags(cmd *cobra.Command) {
	flags := cmd.Flags()
	flags.StringVar(&o.configFile, "config", defaultConfigPath, "file that holds fetchit configuration")
	flags.StringVar(&o.volume, "volume", defaultVolume, "podman volume to hold fetchit data. If volume doesn't exist, fetchit will create it.")
}

// restart fetches new targets from an updated config
// new targets will be added, stale removed, and existing
// will set last commit as last known.
func (hc *FetchitConfig) Restart() {
	hc.scheduler.RemoveByTags(kubeMethod, ansibleMethod, fileTransferMethod, systemdMethod, rawMethod)
	hc.scheduler.Clear()
	hc.InitConfig(false)
	hc.GetTargets()
	hc.RunTargets()
}

func populateConfig(v *viper.Viper) (*FetchitConfig, bool, error) {
	config := NewFetchitConfig()
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
// If not initial, this may be overwritten with what is currently in FETCHIT_CONFIG_URL
func isLocalConfig(v *viper.Viper) (*FetchitConfig, bool, error) {
	if _, err := os.Stat(defaultConfigPath); err != nil {
		klog.Infof("Local config file not found: %v", err)
		return nil, false, err
	}
	return populateConfig(v)
}

// Initconfig reads in config file and env variables if set.
func (hc *FetchitConfig) InitConfig(initial bool) {
	v := viper.New()
	var err error
	var isLocal, exists bool
	var config *FetchitConfig
	envURL := os.Getenv("FETCHIT_CONFIG_URL")

	// user will pass path on local system, but it must be mounted at the defaultConfigPath in fetchit pod
	// regardless of where the config file is on the host, fetchit will read the configFile from within
	// the pod at /opt/mount/fetchit-config.yaml
	if initial && hc.configFile != defaultConfigPath {
		if _, err := os.Stat(defaultConfigPath); err != nil {
			cobra.CheckErr(fmt.Errorf("the local config file must be mounted to /opt/mount directory at /opt/mount/config.yaml in the fetchit pod: %v", err))
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
				cobra.CheckErr(fmt.Errorf("Could not populate config, tried local %s in fetchit pod and also URL: %s", defaultConfigPath, envURL))
			}
			cobra.CheckErr(fmt.Errorf("Error locating config, tried local %s in fetchit pod and also URL %s: %v", defaultConfigPath, envURL, err))
		}
	}

	if config == nil || config.Targets == nil {
		cobra.CheckErr("no fetchit targets found, exiting")
	}

	if config.volume == "" {
		config.volume = defaultVolume
	}

	fetchitVolume = config.volume
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

	if err := detectOrFetchImage(hc.conn, fetchitImage, false); err != nil {
		cobra.CheckErr(err)
	}

	hc.Targets = config.Targets

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
			os.Setenv("FETCHIT_CONFIG_URL", t.Methods.ConfigTarget.ConfigUrl)
		}
		break
	}

	if hc.scheduler == nil {
		hc.scheduler = gocron.NewScheduler(time.UTC)
	}
}

// GetTargets returns map of repoName to map of method:Schedule
func (hc *FetchitConfig) GetTargets() {
	for _, target := range hc.Targets {
		target.mu.Lock()
		defer target.mu.Unlock()
		schedMethods := make(map[string]schedInfo)
		if target.Methods.ConfigTarget != nil {
			schedMethods[configMethod] = schedInfo{
				target.Methods.ConfigTarget.Schedule,
				target.Methods.ConfigTarget.Skew,
			}
		}
		if target.Methods.Raw != nil {
			target.Methods.Raw.initialRun = true
			schedMethods[rawMethod] = schedInfo{
				target.Methods.Raw.Schedule,
				target.Methods.Raw.Skew,
			}
		}
		if target.Methods.Kube != nil {
			target.Methods.Kube.initialRun = true
			schedMethods[kubeMethod] = schedInfo{
				target.Methods.Kube.Schedule,
				target.Methods.Kube.Skew,
			}
		}
		if target.Methods.Systemd != nil {
			target.Methods.Systemd.initialRun = true
			schedMethods[systemdMethod] = schedInfo{
				target.Methods.Systemd.Schedule,
				target.Methods.Systemd.Skew,
			}
			// podman auto-update service is enabled on initialRun regardless of schedule
			if target.Methods.Systemd.Schedule == "" && target.Methods.Systemd.AutoUpdateAll {
				schedMethods[systemdMethod] = schedInfo{
					"*/1 * * * *",
					target.Methods.Systemd.Skew,
				}
			}
		}
		if target.Methods.FileTransfer != nil {
			target.Methods.FileTransfer.initialRun = true
			schedMethods[fileTransferMethod] = schedInfo{
				target.Methods.FileTransfer.Schedule,
				target.Methods.FileTransfer.Skew,
			}
		}
		if target.Methods.Ansible != nil {
			target.Methods.Ansible.initialRun = true
			schedMethods[ansibleMethod] = schedInfo{
				target.Methods.Ansible.Schedule,
				target.Methods.Ansible.Skew,
			}
		}
		if target.Methods.Clean != nil {
			schedMethods[cleanMethod] = schedInfo{
				target.Methods.Clean.Schedule,
				target.Methods.Clean.Skew,
			}
		}
		target.methodSchedules = schedMethods
	}
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (hc *FetchitConfig) RunTargets() {
	allTargets := make(map[string]map[string]schedInfo)
	for _, target := range hc.Targets {
		if target.Url != "" {
			if err := hc.gm.AddTarget(target.Name, target.Url, target.Branch, hc.PAT); err != nil {
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
			skew := 0
			if schedule.Skew != nil {
				skew = rand.Intn(*schedule.Skew)
			}
			switch method {
			case configMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(configMethod).Do(hc.processConfig, ctx, &target, skew)
			case kubeMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(kubeMethod).Do(hc.processKube, ctx, &target, skew)
			case rawMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(rawMethod).Do(hc.processRaw, ctx, &target, skew)
			case systemdMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(systemdMethod).Do(hc.processSystemd, ctx, &target, skew)
			case fileTransferMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(fileTransferMethod).Do(hc.processFileTransfer, ctx, &target, skew)
			case ansibleMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(ansibleMethod).Do(hc.processAnsible, ctx, &target, skew)
			case cleanMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(cleanMethod).Do(hc.processClean, ctx, &target, skew)
			default:
				klog.Warningf("Target: %s Method: %s, unknown method type, ignoring", target.Name, method)
			}
		}
	}
	s.StartImmediately()
	s.StartAsync()
	select {}
}

func (hc *FetchitConfig) processConfig(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	// configUrl in config file will override the environment variable
	config := target.Methods.ConfigTarget
	envURL := os.Getenv("FETCHIT_CONFIG_URL")
	// config.Url from target overrides env variable
	if config.ConfigUrl != "" {
		envURL = config.ConfigUrl
	}
	os.Setenv("FETCHIT_CONFIG_URL", envURL)
	// If ConfigUrl is not populated, warn and leave
	if envURL == "" {
		klog.Warningf("Fetchit ConfigFileTarget found, but neither $FETCHIT_CONFIG_URL on system nor ConfigTarget.ConfigUrl are set, exiting without updating the config.")
	}
	// CheckForConfigUpdates downloads & places config file in defaultConfigPath
	// if the downloaded config file differs from what's currently on the system.
	restart := hc.CheckForConfigUpdates(envURL, true, false)
	if !restart {
		return
	}
	hc.restartFetchit = restart
	if hc.restartFetchit {
		klog.Info("Updated config processed, restarting with new targets")
		fetchitConfig.Restart()
	}
}

func (hc *FetchitConfig) processRaw(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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
		fileName, subDirTree, latest, err := hc.getPathOrTree(target, raw.TargetPath, rawMethod)
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
		hc.gm.SetCurrentWorkingCommit(target.Name, mo.Method, latest)
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		raw.initialRun = hc.resetTarget(target, rawMethod, false, err)
		return
	}
	raw.initialRun = false
}

func (hc *FetchitConfig) processAnsible(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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
		fileName, subDirTree, latest, err := hc.getPathOrTree(target, ans.TargetPath, ansibleMethod)
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
		hc.gm.SetCurrentWorkingCommit(target.Name, mo.Method, latest)
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		ans.initialRun = hc.resetTarget(target, ansibleMethod, false, err)
		return
	}
	ans.initialRun = false
}

func (hc *FetchitConfig) processSystemd(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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
			return
		}
		retry := hc.resetTarget(target, systemdMethod, true, nil)
		if retry {
			return
		}
		fileName, subDirTree, latest, err := hc.getPathOrTree(target, sd.TargetPath, systemdMethod)
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
		hc.gm.SetCurrentWorkingCommit(target.Name, mo.Method, latest)
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		sd.initialRun = hc.resetTarget(target, systemdMethod, false, err)
		return
	}
	sd.initialRun = false
}

func (hc *FetchitConfig) processFileTransfer(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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
		fileName, subDirTree, latest, err := hc.getPathOrTree(target, ft.TargetPath, fileTransferMethod)
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
		hc.gm.SetCurrentWorkingCommit(target.Name, mo.Method, latest)
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		ft.initialRun = hc.resetTarget(target, fileTransferMethod, false, err)
		return
	}
	ft.initialRun = false
}

func (hc *FetchitConfig) processKube(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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
		fileName, subDirTree, latest, err := hc.getPathOrTree(target, kube.TargetPath, kubeMethod)
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
		hc.gm.SetCurrentWorkingCommit(target.Name, mo.Method, latest)
	}

	if err := hc.getChangesAndRunEngine(ctx, mo, path); err != nil {
		kube.initialRun = hc.resetTarget(target, kubeMethod, false, err)
		return
	}
	kube.initialRun = false
}

func (hc *FetchitConfig) processClean(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
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

}

func (hc *FetchitConfig) applyInitial(ctx context.Context, mo *SingleMethodObj, fileName, tp string, tag *[]string, subDirTree *object.Tree) (string, error) {
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

func (hc *FetchitConfig) getChangesAndRunEngine(ctx context.Context, mo *SingleMethodObj, path string) error {
	var targetPath string
	switch mo.Method {
	case rawMethod:
		targetPath = mo.Target.Methods.Raw.TargetPath
	case kubeMethod:
		targetPath = mo.Target.Methods.Kube.TargetPath
	case ansibleMethod:
		targetPath = mo.Target.Methods.Ansible.TargetPath
	case fileTransferMethod:
		targetPath = mo.Target.Methods.FileTransfer.TargetPath
	case systemdMethod:
		targetPath = mo.Target.Methods.Systemd.TargetPath
	default:
		return fmt.Errorf("unknown method: %s", mo.Method)
	}

	tp := targetPath
	if path != "" {
		tp = path
	}

	working, err := hc.gm.GetCurrentWorkingCommit(mo.Target.Name, mo.Method)
	if err != nil {
		return err
	}

	latest, err := hc.gm.GetLatestCommit(mo.Target.Name)
	if err != nil {
		return err
	}

	changes, err := hc.gm.GetDiff(mo.Target.Name, working, latest)
	if err != nil {
		return err
	}

	if changes.Len() == 0 {
		if mo.Method == systemdMethod && mo.Target.Methods.Systemd.Restart && !mo.Target.Methods.Systemd.initialRun {
			return hc.EngineMethod(ctx, mo, filepath.Base(mo.Target.Methods.Systemd.TargetPath), nil)
		}
		klog.Infof("Target: %s, Method: %s: Nothing to pull.....Requeuing", mo.Target.Name, mo.Method)
		return nil
	}

	directory := filepath.Base(mo.Target.Url)
	changeMap := translateChanges(changes, tp, directory)

	ch := make(chan error)
	for change, changePath := range changeMap {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := hc.EngineMethod(ctx, mo, changePath, change); err != nil {
				ch <- utils.WrapErr(err, "error method: %s path: %s, commit: %s", mo.Method, changePath, latest.String())
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changeMap {
		err := <-ch
		if err != nil {
			return err
		}
	}
	hc.gm.SetCurrentWorkingCommit(mo.Target.Name, mo.Method, latest)
	return nil
}

func translateChanges(changes *object.Changes, targetPath, directory string) map[*object.Change]string {
	changesMap := make(map[*object.Change]string)
	for _, change := range *changes {
		if strings.Contains(change.From.Name, targetPath) {
			path := directory + "/" + change.From.Name
			changesMap[change] = path
		} else if strings.Contains(change.To.Name, targetPath) {
			changesMap[change] = deleteFile
		}
	}
	return changesMap
}

// Each engineMethod call now owns the prev and dest variables instead of being shared in mo
func (hc *FetchitConfig) EngineMethod(ctx context.Context, mo *SingleMethodObj, path string, change *object.Change) error {
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

func (hc *FetchitConfig) getPathOrTree(target *Target, subDir, method string) (string, *object.Tree, plumbing.Hash, error) {
	latestHash, err := hc.gm.GetLatestCommit(target.Name)
	if err != nil {
		return "", nil, plumbing.Hash{}, err
	}

	commit, err := hc.gm.GetCommit(target.Name, latestHash)
	if err != nil {
		return "", nil, plumbing.Hash{}, err
	}

	tree, err := commit.Tree()
	if err != nil {
		return "", nil, plumbing.Hash{}, err
	}

	subDirTree, err := tree.Tree(subDir)
	if err != nil {
		if err == object.ErrDirectoryNotFound {
			// check if exact filepath
			file, err := tree.File(subDir)
			if err == nil {
				return file.Name, nil, plumbing.Hash{}, nil
			}
		}
	}
	return "", subDirTree, commit.Hash, err
}

// arrive at resetTarget 1 of 2 ways:
//      1) initial run of target - will return true if clone or commit fetch fails, to try again next run
//      2) processing error during run - will attempt to fetch the remote commit and reset to initialRun true for the
//         next run, or, if fetching of commit fails, will return true to try again next run
// resetTarget returns true if the target should be re-cloned next run, and it will set
func (hc *FetchitConfig) resetTarget(target *Target, method string, initial bool, err error) bool {
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

func (hc *FetchitConfig) getGit(target *Target, initialRun bool) (*object.Commit, error) {
	if initialRun {
		if err := hc.gm.AddTarget(target.Name, target.Url, target.Branch, hc.PAT); err != nil {
			return nil, err
		}
	}

	latestHash, err := hc.gm.GetLatestCommit(target.Name)
	if err != nil {
		return nil, err
	}

	commit, err := hc.gm.GetCommit(target.Name, latestHash)
	if err != nil {
		return nil, err
	}

	return commit, nil
}

// setInitial will return true if fetching of commit fails or results in empty commit, to try again next run
// or, if valid commit is fetched, will set initialRun true and lastCommit for the method, to process next run
func (hc *FetchitConfig) setInitial(target *Target, commit *object.Commit, method string) bool {
	retry := false
	hc.setinitialRun(target, method)
	if commit == nil {
		retry = true
	} else {
		hc.gm.SetCurrentWorkingCommit(target.Name, method, commit.Hash)
	}
	return retry
}

// setinitialRun is called before the initial processing of a target, or
// upon any processing errors for the method, so the method will be retried with next run
func (hc *FetchitConfig) setinitialRun(target *Target, method string) {
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
// in defaultConfigPath in fetchit container (/opt/mount/config.yaml).
// This runs with the initial startup as well as with scheduled ConfigTarget runs,
// if $FETCHIT_CONFIG_URL is set.
func (hc *FetchitConfig) CheckForConfigUpdates(envURL string, existsAlready bool, initial bool) bool {
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
