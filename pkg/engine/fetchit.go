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
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	allMethodTypes     map[string]struct{}
}

func newFetchit() *Fetchit {
	return &Fetchit{
		methodTargetScheds: make(map[Method]SchedInfo),
		allMethodTypes:     make(map[string]struct{}),
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
	for mt := range fetchit.allMethodTypes {
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
			logger.Info("Error with unmarshal of existing config file: %v", err)
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
		if config.ConfigReload.ConfigURL != "" || config.ConfigReload.Device != "" {
			// reset URL if necessary
			// ConfigURL set in config file overrides env variable
			// If the same, this is no change, if diff then the new config has updated the configURL
			os.Setenv("FETCHIT_CONFIG_URL", config.ConfigReload.ConfigURL)
			// Convert configReload to a proper target for processing
			reload := &TargetConfig{
				configReload: config.ConfigReload,
			}
			config.TargetConfigs = append(config.TargetConfigs, reload)
		}
	}
	if config.Prune != nil {
		prune := &TargetConfig{
			prune: config.Prune,
		}
		config.TargetConfigs = append(config.TargetConfigs, prune)
	}
	if config.Images != nil {
		for _, i := range config.Images {
			imageLoad := &TargetConfig{
				image: i,
			}
			config.TargetConfigs = append(config.TargetConfigs, imageLoad)
		}
	}
	if config.PodmanAutoUpdate != nil {
		sysds := config.PodmanAutoUpdate.AutoUpdateSystemd()
		autoUp := &TargetConfig{
			Systemd: sysds,
		}
		config.TargetConfigs = append(config.TargetConfigs, autoUp)
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
		logger.Infof("Local config file not found: %v", err)
		return nil, false, err
	}
	return populateConfig(v)
}

// Initconfig reads in config file and env variables if set.
func (fc *FetchitConfig) InitConfig(initial bool) *Fetchit {
	InitLogger()
	defer logger.Sync()
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

	if config == nil {
		cobra.CheckErr("no fetchit targets found, exiting")
	}

	return fc.populateFetchit(config)
}

func getMethodTargetScheds(targetConfigs []*TargetConfig, fetchit *Fetchit) *Fetchit {
	for _, tc := range targetConfigs {
		tc.mu.Lock()
		defer tc.mu.Unlock()
		internalTarget := &Target{
			url:          tc.Url,
			device:       tc.Device,
			branch:       tc.Branch,
			disconnected: tc.Disconnected,
		}

		if tc.VerifyCommitsInfo != nil {
			internalTarget.gitsignVerify = tc.VerifyCommitsInfo.GitsignVerify
			internalTarget.gitsignRekorURL = tc.VerifyCommitsInfo.GitsignRekorURL
		}

		if tc.configReload != nil {
			tc.configReload.target = internalTarget
			tc.configReload.initialRun = true
			fetchit.methodTargetScheds[tc.configReload] = tc.configReload.SchedInfo()
			fetchit.allMethodTypes[configFileMethod] = struct{}{}
		}

		if tc.prune != nil {
			tc.prune.target = internalTarget
			fetchit.methodTargetScheds[tc.prune] = tc.prune.SchedInfo()
			fetchit.allMethodTypes[pruneMethod] = struct{}{}

		}

		if tc.image != nil {
			tc.image.target = internalTarget
			tc.image.initialRun = true
			fetchit.methodTargetScheds[tc.image] = tc.image.SchedInfo()
			fetchit.allMethodTypes[imageMethod] = struct{}{}

		}

		if len(tc.Ansible) > 0 {
			fetchit.allMethodTypes[ansibleMethod] = struct{}{}
			for _, a := range tc.Ansible {
				a.initialRun = true
				a.target = internalTarget
				fetchit.methodTargetScheds[a] = a.SchedInfo()
			}
		}
		if len(tc.FileTransfer) > 0 {
			fetchit.allMethodTypes[filetransferMethod] = struct{}{}
			for _, ft := range tc.FileTransfer {
				ft.initialRun = true
				ft.target = internalTarget
				fetchit.methodTargetScheds[ft] = ft.SchedInfo()
			}
		}
		if len(tc.Kube) > 0 {
			fetchit.allMethodTypes[kubeMethod] = struct{}{}
			for _, k := range tc.Kube {
				k.initialRun = true
				k.target = internalTarget
				fetchit.methodTargetScheds[k] = k.SchedInfo()
			}
		}
		if len(tc.Raw) > 0 {
			fetchit.allMethodTypes[rawMethod] = struct{}{}
			for _, r := range tc.Raw {
				r.initialRun = true
				r.target = internalTarget
				fetchit.methodTargetScheds[r] = r.SchedInfo()
			}
		}
		if len(tc.Systemd) > 0 {
			fetchit.allMethodTypes[systemdMethod] = struct{}{}
			for _, sd := range tc.Systemd {
				sd.initialRun = true
				sd.target = internalTarget
				fetchit.methodTargetScheds[sd] = sd.SchedInfo()
			}
		}
	}
	return fetchit
}

func (f *Fetchit) RunTargets() {
	for method := range f.methodTargetScheds {
		// ConfigReload, PodmanAutoUpdateAll, Image, Prune methods do not include git URL
		if method.GetTarget().url != "" {
			if err := getRepo(method.GetTarget(), f.pat); err != nil {
				logger.Debugf("Target: %s, clone error: %v, will retry next scheduled run", method.GetTarget(), err)
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
		mt := method.GetKind()
		logger.Infof("Processing git target: %s Method: %s Name: %s", method.GetTarget().url, mt, method.GetName())
		s.Cron(schedInfo.schedule).Tag(mt).Do(method.Process, ctx, f.conn, f.pat, skew)
		s.StartImmediately()
	}
	s.StartAsync()
	select {}
}

func getRepo(target *Target, PAT string) error {
	if target.url != "" && !target.disconnected {
		getClone(target, PAT)
	} else if target.disconnected && len(target.url) > 0 {
		getDisconnected(target)
	} else if target.disconnected && len(target.device) > 0 {
		getDeviceDisconnected(target)
	}
	return nil
}

func getClone(target *Target, PAT string) error {
	directory := getDirectory(target)
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
		logger.Infof("git clone %s %s --recursive", target.url, target.branch)
		var user string
		if PAT != "" {
			user = "fetchit"
		}
		_, err = git.PlainClone(absPath, false, &git.CloneOptions{
			Auth: &githttp.BasicAuth{
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

func getDisconnected(target *Target) error {
	directory := getDirectory(target)
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
		extractZip(target.url)
	}
	return nil
}

func getDeviceDisconnected(target *Target) error {
	directory := getDirectory(target)
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
		localDevicePull(directory, target.device, "", false)
	}
	return nil
}
