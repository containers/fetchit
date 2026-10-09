package engine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
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

var configRestartMu sync.Mutex
var configReloadJobMu sync.Mutex

type Fetchit struct {
	runMu     sync.RWMutex
	retired   bool
	runCancel context.CancelFunc
	lifetime  context.Context
	removals  *removalStore
	// conn holds podman client
	conn context.Context

	ssh       bool
	sshKey    string
	username  string
	password  string
	pat       string
	envSecret string

	scheduler          *gocron.Scheduler
	methodTargetScheds map[Method]SchedInfo
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
	configRestartMu.Lock()
	defer configRestartMu.Unlock()
	if fc.runtimeContext().Err() != nil {
		return
	}
	old := fetchit
	old.retire()
	old.scheduler.Clear()
	if fc.runtimeContext().Err() != nil {
		return
	}
	next := fc.InitConfig(false)
	err := next.startTargets()
	if fc.runtimeContext().Err() == nil {
		cobra.CheckErr(err)
	}
}

// Config reload runs outside this gate so it can wait for deployment jobs to
// finish without waiting on itself. Queued jobs recheck retirement at entry.
func (f *Fetchit) retire() {
	f.runMu.Lock()
	defer f.runMu.Unlock()
	f.retired = true
	if f.runCancel != nil {
		f.runCancel()
	}
}
func (f *Fetchit) runMethod(m Method, ctx, conn context.Context, skew int) {
	f.runMu.RLock()
	if f.retired || ctx.Err() != nil {
		f.runMu.RUnlock()
		return
	}
	if m.GetKind() == configFileMethod {
		f.runMu.RUnlock()
		// Downloads/restarts from overlapping reload jobs must not overwrite one
		// another. Recheck retirement after waiting for the previous reload.
		configReloadJobMu.Lock()
		defer configReloadJobMu.Unlock()
		f.runMu.RLock()
		retired := f.retired
		f.runMu.RUnlock()
		if retired || ctx.Err() != nil {
			return
		}
		status.recordRun(m)
		m.Process(ctx, conn, skew)
		return
	}
	defer f.runMu.RUnlock()
	status.recordRun(m)
	m.Process(ctx, conn, skew)
}

func readConfig(v *viper.Viper) (*FetchitConfig, bool, error) {
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
	if err := validateLifecycleConfig(config); err != nil {
		return nil, false, err
	}
	return config, true, nil
}

func (fc *FetchitConfig) populateFetchit(config *FetchitConfig) *Fetchit {
	fetchit = newFetchit()
	ctx := fc.runtimeContext()
	if fc.conn == nil {
		// TODO: socket directory same for all platforms?
		// sock_dir := os.Getenv("XDG_RUNTIME_DIR")
		// socket := "unix:" + sock_dir + "/podman/podman.sock"
		conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
		if ctx.Err() != nil {
			fetchit.lifetime = ctx
			return fetchit
		}
		if err != nil || conn == nil {
			cobra.CheckErr(fmt.Errorf("error establishing connection to podman.sock: %v", err))
		}
		fc.conn = conn
	}
	fetchit.conn = fc.conn
	fetchit.lifetime = ctx

	if err := detectOrFetchImage(fc.conn, fetchitImage, false); err != nil {
		if ctx.Err() != nil {
			return fetchit
		}
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

	// Check for GitAuth field
	if config.GitAuth != nil {
		// Check for SSH usage
		if config.GitAuth.SSH {
			if err := os.Setenv("SSH_KNOWN_HOSTS", "/opt/mount/.ssh/known_hosts"); err != nil {
				cobra.CheckErr(err)
			}
			keyPath := defaultSSHKey
			// Check for unique ssh key file
			if config.GitAuth.SSHKeyFile != "" {
				keyPath = filepath.Join("/opt", "mount", ".ssh", config.GitAuth.SSHKeyFile)
			}
			if err := checkForPrivateKey(keyPath); err != nil {
				cobra.CheckErr(err)
			}
			fetchit.ssh = true
			fetchit.sshKey = keyPath
		}
		fetchit.username = config.GitAuth.Username
		fetchit.password = config.GitAuth.Password
		fetchit.pat = config.GitAuth.PAT
		fetchit.envSecret = config.GitAuth.EnvSecret
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
	return readConfig(v)
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
		_ = checkForConfigUpdates(envURL, false, true, "", "", "")
	}

	// if config is not yet populated, fc.CheckForConfigUpdates has placed the config
	// downloaded from URL to the defaultconfigPath
	if !isLocal {
		// If not initial run, only way to get here is if already determined need for reload
		// with an updated config placed in defaultConfigPath.
		config, exists, err = readConfig(v)
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

// Takes target from user and converts it for internal use
func getMethodTargetScheds(targetConfigs []*TargetConfig, fetchit *Fetchit) *Fetchit {
	for _, tc := range targetConfigs {
		internalTarget := &Target{
			rollback:        tc.Rollback,
			trackBadCommits: tc.TrackBadCommits,
			url:             tc.Url,
			fallbackURLs:    append([]string(nil), tc.FallbackURLs...),
			device:          tc.Device,
			pat:             fetchit.pat,
			// define the environment variable for envSecret
			envSecret:    fetchit.envSecret,
			ssh:          fetchit.ssh,
			sshKey:       fetchit.sshKey,
			username:     fetchit.username,
			password:     fetchit.password,
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
		}

		if tc.prune != nil {
			tc.prune.target = internalTarget
			fetchit.methodTargetScheds[tc.prune] = tc.prune.SchedInfo()

		}

		if tc.image != nil {
			tc.image.target = internalTarget
			tc.image.initialRun = true
			fetchit.methodTargetScheds[tc.image] = tc.image.SchedInfo()

		}

		if len(tc.Ansible) > 0 {
			for _, a := range tc.Ansible {
				a.initialRun = true
				a.target = internalTarget
				fetchit.methodTargetScheds[a] = a.SchedInfo()
			}
		}
		if len(tc.FileTransfer) > 0 {
			for _, ft := range tc.FileTransfer {
				ft.initialRun = true
				ft.target = internalTarget
				fetchit.methodTargetScheds[ft] = ft.SchedInfo()
			}
		}
		if len(tc.Kube) > 0 {
			for _, k := range tc.Kube {
				k.initialRun = true
				k.target = internalTarget
				fetchit.methodTargetScheds[k] = k.SchedInfo()
			}
		}
		if len(tc.Raw) > 0 {
			for _, r := range tc.Raw {
				r.initialRun = true
				r.target = internalTarget
				fetchit.methodTargetScheds[r] = r.SchedInfo()
			}
		}
		if len(tc.Quadlet) > 0 {
			for _, q := range tc.Quadlet {
				q.initialRun = true
				q.target = internalTarget
				fetchit.methodTargetScheds[q] = q.SchedInfo()
			}
		}

		if len(tc.Systemd) > 0 {
			for _, sd := range tc.Systemd {
				sd.initialRun = true
				sd.target = internalTarget
				fetchit.methodTargetScheds[sd] = sd.SchedInfo()
			}
		}
	}
	for method := range fetchit.methodTargetScheds {
		if normalizer, ok := method.(interface{ normalizeTargetPath() }); ok {
			normalizer.normalizeTargetPath()
		}
	}
	return fetchit
}

func (f *Fetchit) startTargets() error {
	parent := f.lifetime
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return err
	}
	store, err := loadRemovalStore(defaultRemovalsPath)
	if err != nil {
		return err
	}
	if err := store.configure(f.methodTargetScheds); err != nil {
		return err
	}
	f.removals = store
	if err := store.reconcile(f.conn); err != nil {
		logger.Errorf("Method removal cleanup: %v", err)
	}
	ctx, cancel := context.WithCancel(parent)
	f.runCancel = cancel
	status.replace(f.methodTargetScheds)
	startStatusServer()
	for method := range f.methodTargetScheds {
		if err := ctx.Err(); err != nil {
			return err
		}
		// ConfigReload, PodmanAutoUpdateAll, Image, Prune methods do not include git URL
		if method.GetTarget().url != "" {
			if err := getRepo(method.GetTarget()); err != nil {
				logger.Debugf("Target: %s, clone error: %v, will retry next scheduled run", method.GetTarget(), err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s := f.scheduler
	for method, schedInfo := range f.methodTargetScheds {
		skew := 0
		if schedInfo.skew != nil {
			skew = rand.Intn(*schedInfo.skew)
		}
		mt := method.GetKind()
		logger.Infof("Processing git target: %s Method: %s Name: %s", method.GetTarget().url, mt, method.GetName())
		m := method
		s.Cron(schedInfo.schedule).Tag(mt).Do(func(ctx, conn context.Context, skew int) {
			f.runMethod(m, ctx, conn, skew)
		}, ctx, f.conn, skew)
		s.StartImmediately()
	}
	if _, err := s.Every(1).Minute().Tag("removal-cleanup").Do(func() {
		f.runMu.RLock()
		defer f.runMu.RUnlock()
		if f.retired || ctx.Err() != nil {
			return
		}
		if err := store.reconcile(f.conn); err != nil {
			logger.Errorf("Method removal cleanup retry: %v", err)
		}
	}); err != nil {
		return err
	}
	s.StartAsync()
	return nil
}

func getRepo(target *Target) error {
	if target.url != "" && !target.disconnected {
		return getClone(target)
	} else if target.disconnected && len(target.url) > 0 {
		return getDisconnected(target)
	} else if target.disconnected && len(target.device) > 0 {
		return getDeviceDisconnected(target)
	}
	return nil
}

func getClone(target *Target) error {
	unlock := lockRepositoryCache(target)
	defer unlock()
	if hasRepositoryMirrors(target) {
		return getCloneWithMirrors(target)
	}
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
		// if the envSecret is set, use it as variable target.PAT
		if target.envSecret != "" {
			target.pat = os.Getenv(target.envSecret)
			logger.Infof("Using the envSecret %s", target.envSecret)
		}
		if target.pat != "" {
			target.username = "fetchit"
			target.password = target.pat
		}
		// default to using existing http method
		cOptions := &git.CloneOptions{
			Auth: &githttp.BasicAuth{
				Username: target.username, // the value of this field should not matter when using a PAT
				Password: target.password,
			},
			URL:           target.url,
			ReferenceName: plumbing.ReferenceName(fmt.Sprintf("refs/heads/%s", target.branch)),
			SingleBranch:  true,
		}
		// if using ssh, change auth to use ssh key
		if target.ssh {
			logger.Infof("git clone %s using SSH key %s ", target.url, target.sshKey)
			authValue, err := ssh.NewPublicKeysFromFile("git", target.sshKey, target.password)
			if err != nil {
				logger.Infof("generate publickeys failed: %s", err.Error())
				return err
			}
			cOptions.Auth = authValue
		}
		_, err := git.PlainClone(absPath, false, cOptions)
		if err != nil {
			logger.Infof("git clone failed: %s", err.Error())
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
		return extractZip(target.url)
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
		_, err := localDevicePull(directory, target.device, "", false)
		return err
	}
	return nil
}
