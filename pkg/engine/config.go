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

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const configFileMethod = "config"

// ConfigReload configures a target for dynamic loading of fetchit config updates
// $FETCHIT_CONFIG_URL environment variable or a local file with a ConfigReload target
// at ~/.fetchit/config.yaml will inform fetchit to use this target.
// Without this target, fetchit will not watch for config updates.
// At this time, only 1 FetchitConfigReload target can be passed to fetchit
// TODO: Collect multiple from multiple FetchitTargets and merge configs into 1 on disk
type ConfigReload struct {
	CommonMethod `mapstructure:",squash"`
	ConfigURL    string `mapstructure:"configURL"`
	Device       string `mapstructure:"device"`
	ConfigPath   string `mapstructure:"configPath"`
}

func (c *ConfigReload) GetKind() string {
	return configFileMethod
}

func (c *ConfigReload) GetName() string {
	return configFileMethod
}

func (c *ConfigReload) Process(ctx, conn context.Context, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	// configURL in config file will override the environment variable
	envURL := os.Getenv("FETCHIT_CONFIG_URL")
	// config.URL from target overrides env variable
	if c.ConfigURL != "" {
		envURL = c.ConfigURL
	}
	os.Setenv("FETCHIT_CONFIG_URL", envURL)
	// If ConfigURL is not populated, warn and leave
	if envURL == "" && c.Device == "" {
		logger.Debugf("Fetchit ConfigReload found, but neither $FETCHIT_CONFIG_URL on system nor ConfigReload.ConfigURL are set, exiting without updating the config.")
	}
	// CheckForConfigUpdates downloads & places config file in defaultConfigPath
	// if the downloaded config file differs from what's currently on the system.
	if envURL != "" {
		restart := checkForConfigUpdates(envURL, true, false)
		if !restart {
			return
		}
		logger.Info("Updated config processed, restarting with new targets")
		fetchitConfig.Restart()
	} else if c.Device != "" {
		restart := checkForDisconUpdates(c.Device, c.ConfigPath, true, false)
		if !restart {
			return
		}
		logger.Info("Updated config processed, restarting with new targets")
		fetchitConfig.Restart()
	}

}

func (c *ConfigReload) MethodEngine(ctx, conn context.Context, change *object.Change, path string) error {
	return nil
}

func (c *ConfigReload) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	return nil
}

// checkForConfigUpdates downloads & places config file
// in defaultConfigPath in fetchit container (/opt/mount/config.yaml).
// This runs with the initial startup as well as with scheduled ConfigReload runs,
// if $FETCHIT_CONFIG_URL is set.
func checkForConfigUpdates(envURL string, existsAlready bool, initial bool) bool {
	// envURL is either set by user or set to match a configURL in a configReload
	if envURL == "" {
		return false
	}
	reset, err := downloadUpdateConfigFile(envURL, existsAlready, initial)
	if err != nil {
		logger.Info(err)
	}
	return reset
}

// CheckForDisconUpdates identifies if the device is connected and if a cache file exists
func checkForDisconUpdates(device, configPath string, existsAlready bool, initial bool) bool {
	ctx := context.Background()
	name := "fetchit-config"
	cache := "/opt/.cache/" + name
	dest := cache + "/" + "config.yaml"
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		logger.Error("Failed to create connection to podman")
		return false
	}
	// Ensure that the device is present
	_, exitCode, err := localDeviceCheck(name, device, "")
	if err != nil {
		logger.Error("Failed to check device")
		return false
	}
	if exitCode != 0 {
		// remove the diff file
		err = os.Remove(dest)
		logger.Info("Device not present...requeuing")
		return false
	} else if exitCode == 0 {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			// make the cache directory
			err = os.MkdirAll(cache, 0755)
			copyFile := ("/mnt/" + configPath + " " + dest)
			s := generateDeviceSpec(filetransferMethod, "disconnected-", copyFile, device, name)
			createResponse, err := createAndStartContainer(conn, s)
			if err != nil {
				return false
			}
			// Wait for the container to finish
			waitAndRemoveContainer(conn, createResponse.ID)
			logger.Info("container created", createResponse.ID)
			currentConfigBytes, err := ioutil.ReadFile(defaultConfigPath)
			newBytes, err := ioutil.ReadFile(dest)
			if err != nil {
				logger.Error("Failed to read config file")
			} else {
				if bytes.Equal(newBytes, currentConfigBytes) {
					return false
				} else {
					// Replace the old config file at defaultConfigPath with the new one from dest and restart
					os.WriteFile(defaultConfigBackup, currentConfigBytes, 0600)
					os.WriteFile(defaultConfigPath, newBytes, 0600)
					logger.Infof("Current config backup placed at %s", defaultConfigBackup)
					return true
				}
			}
		}
	}
	return false
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
			logger.Infof("unable to read current config, will try with new downloaded config file: %v", err)
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
			logger.Infof("Current config backup placed at %s", defaultConfigBackup)
		}
	}
	if err := os.WriteFile(defaultConfigPath, newBytes, 0600); err != nil {
		return false, fmt.Errorf("unable to write new config contents, reverting to old config: %v", err)
	}

	logger.Infof("Config updates found from url: %s, will load new targets", urlStr)
	return true, nil
}
