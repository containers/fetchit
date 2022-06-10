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
	fetchitService = "fetchit"
	fetchitVolume  = "fetchit-volume"
	fetchitImage   = "quay.io/fetchit/fetchit:latest"
	deleteFile     = "delete"
)

var (
	defaultConfigPath   = filepath.Join("/opt", "mount", "config.yaml")
	defaultConfigBackup = filepath.Join("/opt", "mount", "config-backup.yaml")

	fetchitConfig *FetchitConfig
	fetchit       *Fetchit
)

type Fetchit struct {
	// conn holds podman client
	conn               context.Context
	volume             string
	pat                string
	restartFetchit     bool
	scheduler          *gocron.Scheduler
	methodTargetScheds map[Method]SchedInfo
	allMethodTypes     []string
}

func newFetchit() *Fetchit {
	return &Fetchit{
		methodTargetScheds: make(map[Method]SchedInfo),
	}
}

func newFetchitConfig() *FetchitConfig {
	return &FetchitConfig{
		TargetConfigs: []*TargetConfig{},
	}
}

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

// restart fetches new targets from an updated config
// new targets will be added, stale removed, and existing
// will set last commit as last known.
func (fc *FetchitConfig) Restart() {
	for _, mt := range fetchit.allMethodTypes {
		fetchit.scheduler.RemoveByTags(mt)
	}
	fetchit.scheduler.Clear()
	fetchit = fc.InitConfig(false)
	fetchit.RunTargets()
}

func populateConfig(v *viper.Viper) (*FetchitConfig, bool, error) {
	config := newFetchitConfig()
	configDir := filepath.Dir(defaultConfigPath)
	configName := filepath.Base(defaultConfigPath)
	v.AddConfigPath(configDir)
	v.SetConfigName(configName)
	v.SetConfigType("yaml")

	if err := v.ReadInConfig(); err == nil {
		if err := v.Unmarshal(&config); err != nil {
			klog.Info("Error with unmarshal of existing config file: %v", err)
			return nil, false, err
		}
	}
	return config, true, nil
}

func (fc *FetchitConfig) populateFetchit(config *FetchitConfig) *Fetchit {
	fetchit = newFetchit()
	fetchit.pat = fc.PAT
	ctx := context.Background()
	if fc.conn == nil {
		// TODO: socket directory same for all platforms?
		// sock_dir := os.Getenv("XDG_RUNTIME_DIR")
		// socket := "unix:" + sock_dir + "/podman/podman.sock"
		conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
		if err != nil || conn == nil {
			cobra.CheckErr(fmt.Errorf("error establishing connection to podman.sock: %v", err))
		}
		fc.conn = conn
	}
	fetchit.conn = fc.conn

	if err := detectOrFetchImage(fc.conn, fetchitImage, false); err != nil {
		cobra.CheckErr(err)
	}

	// look for a ConfigURL, only find the first
	// TODO: add logic to merge multiple configs
	if config.ConfigReload != nil {
		if config.ConfigReload.ConfigURL != "" {
			// reset URL if necessary
			// ConfigURL set in config file overrides env variable
			// If the same, this is no change, if diff then the new config has updated the configURL
			os.Setenv("FETCHIT_CONFIG_URL", config.ConfigReload.ConfigURL)
			// Convert configReload to a proper target for processing
			reload := &TargetConfig{
				Name:         configFileMethod,
				configReload: config.ConfigReload,
			}
			config.TargetConfigs = append(config.TargetConfigs, reload)
		}
	}

	fc.TargetConfigs = config.TargetConfigs
	if fc.scheduler == nil {
		fc.scheduler = gocron.NewScheduler(time.UTC)
	}
	fetchit.scheduler = fc.scheduler
	return getMethodTargetScheds(fc.TargetConfigs, fetchit)
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
func (fc *FetchitConfig) InitConfig(initial bool) *Fetchit {
	v := viper.New()
	var err error
	var isLocal, exists bool
	var config *FetchitConfig
	envURL := os.Getenv("FETCHIT_CONFIG_URL")

	// user will pass path on local system, but it must be mounted at the defaultConfigPath in fetchit pod
	// regardless of where the config file is on the host, fetchit will read the configFile from within
	// the pod at /opt/mount
	if initial {
		if _, err := os.Stat(filepath.Dir(defaultConfigPath)); err != nil {
			if envURL == "" {
				cobra.CheckErr(fmt.Errorf("the local config file must be mounted to /opt/mount directory at /opt/mount/config.yaml in the fetchit pod: %v", err))
			}
		}
	}

	config, isLocal, err = isLocalConfig(v)
	if (initial && !isLocal) || err != nil {
		// Only run this from initial startup and only after trying to populate the config from a local file.
		// because CheckForConfigUpdates also runs with each processConfig, so if !initial this is already done
		// If configURL is passed in, a config file on disk has priority on the initial run.
		_ = checkForConfigUpdates(envURL, false, true)
	}

	// if config is not yet populated, fc.CheckForConfigUpdates has placed the config
	// downloaded from URL to the defaultconfigPath
	if !isLocal {
		// If not initial run, only way to get here is if already determined need for reload
		// with an updated config placed in defaultConfigPath.
		config, exists, err = populateConfig(v)
		if config == nil || !exists || err != nil {
			if err != nil {
				cobra.CheckErr(fmt.Errorf("Could not populate config, tried %s in fetchit pod and also URL: %s. Ensure local config is mounted or served from a URL and try again.", defaultConfigPath, envURL))
			}
			cobra.CheckErr(fmt.Errorf("Error locating config, tried %s in fetchit pod and also URL %s. Ensure local config is mounted or served from a URL and try again: %v", defaultConfigPath, envURL, err))
		}
	}

	if config == nil || (config.TargetConfigs == nil && config.ConfigReload == nil) {
		cobra.CheckErr("no fetchit targets found, exiting")
	}

	return fc.populateFetchit(config)
}

func getMethodTargetScheds(targetConfigs []*TargetConfig, fetchit *Fetchit) *Fetchit {
	for _, tc := range targetConfigs {
		tc.mu.Lock()
		defer tc.mu.Unlock()
		gitTarget := &Target{
			Name:   tc.Name,
			url:    tc.Url,
			branch: tc.Branch,
		}
		if tc.configReload != nil {
			tc.configReload.initialRun = true
			fetchit.methodTargetScheds[tc.configReload] = tc.configReload.SchedInfo()
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, configFileMethod)
		}

		if tc.Clean != nil {
			fetchit.methodTargetScheds[tc.Clean] = tc.Clean.SchedInfo()
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, cleanMethod)
		}

		if tc.Ansible != nil {
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, ansibleMethod)
			for _, a := range tc.Ansible {
				a.initialRun = true
				a.target = gitTarget
				fetchit.methodTargetScheds[a] = a.SchedInfo()
			}
		}
		if tc.FileTransfer != nil {
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, filetransferMethod)
			for _, ft := range tc.FileTransfer {
				ft.initialRun = true
				ft.target = gitTarget
				fetchit.methodTargetScheds[ft] = ft.SchedInfo()
			}
		}
		if tc.Kube != nil {
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, kubeMethod)
			for _, k := range tc.Kube {
				k.initialRun = true
				k.target = gitTarget
				fetchit.methodTargetScheds[k] = k.SchedInfo()
			}
		}
		if tc.Raw != nil {
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, rawMethod)
			for _, r := range tc.Raw {
				r.initialRun = true
				r.target = gitTarget
				fetchit.methodTargetScheds[r] = r.SchedInfo()
			}
		}
		if tc.Systemd != nil {
			fetchit.allMethodTypes = append(fetchit.allMethodTypes, systemdMethod)
			for _, sd := range tc.Systemd {
				sd.initialRun = true
				sd.target = gitTarget
				fetchit.methodTargetScheds[sd] = sd.SchedInfo()
			}
		}
	}
	return fetchit
}

// This assumes each Target has no more than 1 each of Raw, Systemd, FileTransfer
func (f *Fetchit) RunTargets() {
	for method := range f.methodTargetScheds {
		// ConfigReload, Systemd.AutoUpdateAll, Clean methods do not include git URL
		if method.Target().url != "" {
			if err := getClone(method.Target(), f.pat); err != nil {
				klog.Warningf("Target: %s, clone error: %v, will retry next scheduled run", method.Target().Name, err)
			}
		}
	}

	s := f.scheduler
	for method, schedInfo := range f.methodTargetScheds {
		skew := 0
		if schedInfo.skew != nil {
			skew = rand.Intn(*schedInfo.skew)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		mt := method.Type()
		klog.Infof("Processing Target: %s Method: %s Name: %s", method.Target().Name, mt, method.GetName())
		s.Cron(schedInfo.schedule).Tag(mt).Do(method.Process, ctx, f.conn, f.pat, skew)
		s.StartImmediately()
	}
	s.StartAsync()
	select {}
}

func getClone(target *Target, PAT string) error {
	directory := filepath.Base(target.url)
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
		klog.Infof("git clone %s %s --recursive", target.url, target.branch)
		var user string
		if PAT != "" {
			user = "fetchit"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &http.BasicAuth{
				Username: user, // the value of this field should not matter when using a PAT
				Password: PAT,
			},
			URL:           target.url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", target.branch)),
			SingleBranch:  true,
		})
		if err != nil {
			return err
		}
	}
	return nil
}
