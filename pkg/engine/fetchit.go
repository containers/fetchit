package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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
}

func NewFetchitConfig() *FetchitConfig {
	return &FetchitConfig{
		Targets: []*Target{
			{
				methodSchedules: make(map[string]schedInfo),
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
				s.StartImmediately()
			case kubeMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(kubeMethod).Do(hc.processKube, ctx, &target, skew)
				s.StartImmediately()
			case rawMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(rawMethod).Do(hc.processRaw, ctx, &target, skew)
				s.StartImmediately()
			case systemdMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(systemdMethod).Do(hc.processSystemd, ctx, &target, skew)
				s.StartImmediately()
			case fileTransferMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Target: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(fileTransferMethod).Do(hc.processFileTransfer, ctx, &target, skew)
				s.StartImmediately()
			case ansibleMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(ansibleMethod).Do(hc.processAnsible, ctx, &target, skew)
				s.StartImmediately()
			case cleanMethod:
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				klog.Infof("Processing Repo: %s Method: %s", target.Name, method)
				s.Cron(schedule.Schedule).Tag(cleanMethod).Do(hc.processClean, ctx, &target, skew)
				s.StartImmediately()
			default:
				klog.Warningf("Target: %s Method: %s, unknown method type, ignoring", target.Name, method)
			}
		}
	}
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
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: rawMethod,
		Target: target,
	}

	if initial {
		err := hc.getClone(target)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := hc.GetLatest(mo.Target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := hc.GetCurrent(mo.Target, mo.Method)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = hc.Apply(ctx, mo, current, latest, mo.Target.Methods.Raw.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		hc.UpdateCurrent(ctx, target, mo.Method, latest)
		klog.Infof("Moved raw from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, raw currently at %s", target.Name, current)
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
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: ansibleMethod,
		Target: target,
	}
	if initial {
		err := hc.getClone(target)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := hc.GetLatest(mo.Target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := hc.GetCurrent(mo.Target, mo.Method)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = hc.Apply(ctx, mo, current, latest, mo.Target.Methods.Ansible.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		hc.UpdateCurrent(ctx, target, mo.Method, latest)
		klog.Infof("Moved ansible from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, ansible currently at %s", target.Name, current)
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
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: systemdMethod,
		Target: target,
	}
	tag := []string{".service"}
	if sd.Restart {
		sd.Enable = true
	}
	if initial {
		if sd.AutoUpdateAll {
			if err := hc.EngineMethod(ctx, mo, "", nil); err != nil {
				klog.Infof("Failed to start podman-auto-update.service: %v", err)
			}
			sd.initialRun = false
			return
		}
		err := hc.getClone(target)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := hc.GetLatest(mo.Target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := hc.GetCurrent(mo.Target, mo.Method)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = hc.Apply(ctx, mo, current, latest, mo.Target.Methods.Systemd.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		hc.UpdateCurrent(ctx, target, mo.Method, latest)
		klog.Infof("Moved systemd from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, systemd currently at %s", target.Name, current)
	}

	sd.initialRun = false
}

func (hc *FetchitConfig) processFileTransfer(ctx context.Context, target *Target, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	ft := target.Methods.FileTransfer
	initial := ft.initialRun
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: fileTransferMethod,
		Target: target,
	}
	if initial {
		err := hc.getClone(target)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := hc.GetLatest(mo.Target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := hc.GetCurrent(mo.Target, mo.Method)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = hc.Apply(ctx, mo, current, latest, mo.Target.Methods.FileTransfer.TargetPath, nil)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		hc.UpdateCurrent(ctx, target, mo.Method, latest)
		klog.Infof("Moved filetransfer from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, filetransfer currently at %s", target.Name, current)
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
	mo := &SingleMethodObj{
		Conn:   hc.conn,
		Method: kubeMethod,
		Target: target,
	}

	if initial {
		err := hc.getClone(target)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.Url, target.Name, err)
			return
		}
	}

	latest, err := hc.GetLatest(mo.Target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := hc.GetCurrent(mo.Target, mo.Method)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = hc.Apply(ctx, mo, current, latest, mo.Target.Methods.Kube.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		hc.UpdateCurrent(ctx, target, mo.Method, latest)
		klog.Infof("Moved kube from %s to %s for target %s", current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, kube currently at %s", target.Name, current)
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

func (hc *FetchitConfig) getClone(target *Target) error {
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
			user = "fetchit"
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
