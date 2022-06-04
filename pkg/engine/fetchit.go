package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"k8s.io/klog/v2"
)

const (
	fetchitService   = "fetchit"
	defaultVolume    = "fetchit-volume"
	defaultConfigURL = ""
	fetchitImage     = "quay.io/fetchit/fetchit:latest"

	deleteFile = "delete"
)

var (
	defaultConfigPath   = filepath.Join("/opt", "mount", "config.yaml")
	defaultConfigBackup = filepath.Join("/opt", "mount", "config-backup.yaml")
)

// FetchitConfig requires necessary objects to process targets
type FetchitConfig struct {
	// Conn holds podman client
	Conn           context.Context
	RestartFetchit bool
	Targets        []*Target     `mapstructure:"targets"`
	ConfigTarget   *ConfigTarget `mapstructure:"configTarget"`
	PAT            string        `mapstructure:"pat"`
	volume         string        `mapstructure:"volume"`
	scheduler      *gocron.Scheduler
	configFile     string
	allMethodTypes []string
}

func NewFetchitConfig() *FetchitConfig {
	return &FetchitConfig{
		Targets: []*Target{
			{
				methodSchedules: make(map[string]SchedInfo),
			},
		},
	}
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
func (fc *FetchitConfig) Restart() {
	for _, mt := range fc.allMethodTypes {
		fc.scheduler.RemoveByTags(mt)
	}
	fc.scheduler.Clear()
	fc.InitConfig(false)
	fc.GetTargets()
	fc.RunTargets()
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
func (fc *FetchitConfig) InitConfig(initial bool) {
	v := viper.New()
	var err error
	var isLocal, exists bool
	var config *FetchitConfig
	envURL := os.Getenv("FETCHIT_CONFIG_URL")

	// user will pass path on local system, but it must be mounted at the defaultConfigPath in fetchit pod
	// regardless of where the config file is on the host, fetchit will read the configFile from within
	// the pod at /opt/mount/fetchit-config.yaml
	if initial && fc.configFile != defaultConfigPath {
		if _, err := os.Stat(defaultConfigPath); err != nil {
			cobra.CheckErr(fmt.Errorf("the local config file must be mounted to /opt/mount directory at /opt/mount/config.yaml in the fetchit pod: %v", err))
		}
	}

	config, isLocal, err = isLocalConfig(v)
	if (initial && !isLocal) || err != nil {
		// Only run this from initial startup and only after trying to populate the config from a local file.
		// because CheckForConfigUpdates also runs with each processConfig, so if !initial this is already done
		// If configURL is passed in, a config file on disk has priority on the initial run.
		_ = fc.CheckForConfigUpdates(envURL, false, true)
	}

	// if config is not yet populated, fc.CheckForConfigUpdates has placed the config
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

	if config == nil || (config.Targets == nil && config.ConfigTarget == nil) {
		cobra.CheckErr("no fetchit targets found, exiting")
	}

	if config.volume == "" {
		config.volume = defaultVolume
	}

	fetchitVolume = config.volume
	ctx := context.Background()
	if fc.Conn == nil {
		// TODO: socket directory same for all platforms?
		// sock_dir := os.Getenv("XDG_RUNTIME_DIR")
		// socket := "unix:" + sock_dir + "/podman/podman.sock"
		conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
		if err != nil || conn == nil {
			cobra.CheckErr(fmt.Errorf("error establishing connection to podman.sock: %v", err))
		}
		fc.Conn = conn
	}

	if err := detectOrFetchImage(fc.Conn, fetchitImage, false); err != nil {
		cobra.CheckErr(err)
	}

	// look for a ConfigURL, only find the first
	// TODO: add logic to merge multiple configs
	if config.ConfigTarget != nil {
		if config.ConfigTarget.ConfigURL != "" {
			// reset URL if necessary
			// ConfigURL set in config file overrides env variable
			// If the same, this is no change, if diff then the new config has updated the configURL
			os.Setenv("FETCHIT_CONFIG_URL", config.ConfigTarget.ConfigURL)
			configTarget := &Target{
				Name:         configFileMethod,
				ConfigReload: config.ConfigTarget,
			}
			config.Targets = append(config.Targets, configTarget)
		}
	}

	fc.Targets = config.Targets
	if fc.scheduler == nil {
		fc.scheduler = gocron.NewScheduler(time.UTC)
	}
}

// getMethods populates FetchitConfig.Methods
func getMethods(t *Target) {
	if t.ConfigReload != nil {
		t.Methods = append(t.Methods, t.ConfigReload)
	}
	if t.Clean != nil {
		t.Methods = append(t.Methods, t.Clean)
	}
	if t.Ansible != nil {
		for _, a := range t.Ansible {
			t.Methods = append(t.Methods, a)
		}
	}
	if t.FileTransfer != nil {
		for _, ft := range t.FileTransfer {
			t.Methods = append(t.Methods, ft)
		}
	}
	if t.Kube != nil {
		for _, k := range t.Kube {
			t.Methods = append(t.Methods, k)
		}
	}
	if t.Raw != nil {
		for _, r := range t.Raw {
			t.Methods = append(t.Methods, r)
		}
	}
	if t.Systemd != nil {
		for _, sd := range t.Systemd {
			if sd.AutoUpdateAll {
				sd.Name = podmanAutoUpdate
			}
			t.Methods = append(t.Methods, sd)
		}
	}
}

func (fc *FetchitConfig) GetTargets() {
	for _, t := range fc.Targets {
		t.mu.Lock()
		defer t.mu.Unlock()
		getMethods(t)
		schedMethods := make(map[string]SchedInfo)
		for _, m := range t.Methods {
			fc.allMethodTypes = append(fc.allMethodTypes, m.Type())
			schedMethods[m.GetName()] = m.SchedInfo()
			t.methodSchedules = schedMethods
		}
	}
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (fc *FetchitConfig) RunTargets() {
	allTargets := make(map[string]map[string]SchedInfo)
	for _, target := range fc.Targets {
		// ConfigTarget does not include git URL
		if target.Url != "" {
			if err := getClone(target, fc.PAT); err != nil {
				klog.Warningf("Target: %s, clone error: %v, will retry next scheduled run", target.Name, err)
			}
		}
		allTargets[target.Name] = target.methodSchedules
	}

	s := fc.scheduler
	for repoName, schedMethods := range allTargets {
		var target *Target
		for _, t := range fc.Targets {
			if repoName == t.Name {
				target = t
			}
		}

		for singleMethodName, schedule := range schedMethods {
			var method Method
			for _, m := range target.Methods {
				if m.GetName() == singleMethodName {
					method = m
				}
			}
			skew := 0
			if schedule.Skew != nil {
				skew = rand.Intn(*schedule.Skew)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mt := method.Type()
			klog.Infof("Processing Target: %s Method: %s Name: %s", target.Name, mt, method.GetName())
			s.Cron(schedule.Schedule).Tag(mt).Do(method.Process, ctx, fc.Conn, target, fc.PAT, skew)
			s.StartImmediately()
		}
	}
	s.StartAsync()
	select {}
}

func getClone(target *Target, PAT string) error {
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
		if PAT != "" {
			user = "fetchit"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user, // the value of this field should not matter when using a PAT
				Password: PAT,
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

// CheckForConfigUpdates, downloads, & places config file
// in defaultConfigPath in fetchit container (/opt/mount/config.yaml).
// This runs with the initial startup as well as with scheduled ConfigTarget runs,
// if $FETCHIT_CONFIG_URL is set.
func (fc *FetchitConfig) CheckForConfigUpdates(envURL string, existsAlready bool, initial bool) bool {
	// envURL is either set by user or set to match a configURL in a configTarget
	if envURL == "" {
		return false
	}
	reset, err := downloadUpdateConfigFile(envURL, existsAlready, initial)
	if err != nil {
		klog.Info(err)
	}
	return reset
}
