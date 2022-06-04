package engine

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

const configFileMethod = "config"

// ConfigTarget configures a target for dynamic loading of fetchit config updates
// $FETCHIT_CONFIG_URL environment variable or a local file with a ConfigTarget target
// at ~/.fetchit/config.yaml will inform fetchit to use this target.
// Without this target, fetchit will not watch for config updates.
// At this time, only 1 FetchitConfigTarget target can be passed to fetchit
// TODO: Collect multiple from multiple FetchitTargets and merge configs into 1 on disk
type ConfigTarget struct {
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// URL location of config file, such as a raw github URL
	ConfigURL string `mapstructure:"configURL"`
	// initialRun is set by fetchit
	initialRun bool
}

func (c *ConfigTarget) Type() string {
	return configFileMethod
}

func (c *ConfigTarget) GetName() string {
	return configFileMethod
}

func (c *ConfigTarget) SchedInfo() SchedInfo {
	return SchedInfo{
		Schedule: c.Schedule,
		Skew:     c.Skew,
	}
}
func (c *ConfigTarget) Process(ctx, conn context.Context, target *Target, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	// configURL in config file will override the environment variable
	envURL := os.Getenv("FETCHIT_CONFIG_URL")
	// config.URL from target overrides env variable
	if c.ConfigURL != "" {
		envURL = c.ConfigURL
	}
	os.Setenv("FETCHIT_CONFIG_URL", envURL)
	// If ConfigURL is not populated, warn and leave
	if envURL == "" {
		klog.Warningf("Fetchit ConfigTarget found, but neither $FETCHIT_CONFIG_URL on system nor ConfigTarget.ConfigURL are set, exiting without updating the config.")
	}
	// CheckForConfigUpdates downloads & places config file in defaultConfigPath
	// if the downloaded config file differs from what's currently on the system.
	restart := fetchitConfig.CheckForConfigUpdates(envURL, true, false)
	if !restart {
		return
	}
	klog.Info("Updated config processed, restarting with new targets")
	fetchitConfig.Restart()
}

func (c *ConfigTarget) MethodEngine(ctx, conn context.Context, change *object.Change, path string) error {
	return nil
}

func (c *ConfigTarget) Apply(ctx, conn context.Context, target *Target, currentState, desiredState plumbing.Hash, targetPath string, tags *[]string) error {
	return nil
}

// downloadUpdateConfig returns true if config was updated in fetchit pod
func downloadUpdateConfigFile(urlStr string, existsAlready, initial bool) (bool, error) {
	_, err := url.Parse(urlStr)
	if err != nil {
		return false, fmt.Errorf("unable to parse config file url %s: %v", urlStr, err)
	}
	client := http.Client{
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			r.URL.Opaque = r.URL.Path
			return nil
		},
	}
	resp, err := client.Get(urlStr)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	newBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, fmt.Errorf("error downloading config from %s: %v", err)
	}
	if newBytes == nil {
		// if initial, this is the last resort, newBytes should be populated
		// the only way to get here from initial
		// is if there is no config file on disk, only a FETCHIT_CONFIG_URL
		return false, fmt.Errorf("found empty config at %s, unable to update or populate config", urlStr)
	}
	if !initial {
		currentConfigBytes, err := ioutil.ReadFile(defaultConfigPath)
		if err != nil {
			klog.Infof("unable to read current config, will try with new downloaded config file: %v", err)
			existsAlready = false
		} else {
			if bytes.Equal(newBytes, currentConfigBytes) {
				return false, nil
			}
		}

		if existsAlready {
			if err := os.WriteFile(defaultConfigBackup, currentConfigBytes, 0600); err != nil {
				return false, fmt.Errorf("could not copy %s to path %s: %v", defaultConfigPath, defaultConfigBackup, err)
			}
			klog.Infof("Current config backup placed at %s", defaultConfigBackup)
		}
	}
	if err := os.WriteFile(defaultConfigPath, newBytes, 0600); err != nil {
		return false, fmt.Errorf("unable to write new config contents, reverting to old config: %v", err)
	}

	klog.Infof("Config updates found from url: %s, will load new targets", urlStr)
	return true, nil
}
