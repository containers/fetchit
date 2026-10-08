package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/containers/podman/v5/pkg/bindings"
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
	GitAuth      `mapstructure:",squash"`
}

func (c *ConfigReload) GetKind() string {
	return configFileMethod
}

func (c *ConfigReload) GetName() string {
	return configFileMethod
}

func (c *ConfigReload) Process(ctx, conn context.Context, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	// configURL in config file will override the environment variable
	envURL := os.Getenv("FETCHIT_CONFIG_URL")
	// config.URL from target overrides env variable
	if c.ConfigURL != "" {
		envURL = c.ConfigURL
	}
	pat := fetchit.pat
	if fetchit.envSecret != "" {
		pat = os.Getenv(fetchit.envSecret)
	}
	username := fetchit.username
	password := fetchit.password
	// If ConfigURL is not populated, warn and leave
	if envURL == "" && c.Device == "" {
		logger.Debugf("Fetchit ConfigReload found, but neither $FETCHIT_CONFIG_URL on system nor ConfigReload.ConfigURL are set, exiting without updating the config.")
	}
	// CheckForConfigUpdates downloads & places config file in defaultConfigPath
	// if the downloaded config file differs from what's currently on the system.
	if envURL != "" {
		restart := checkForConfigUpdates(envURL, true, false, pat, username, password)
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
func checkForConfigUpdates(envURL string, existsAlready bool, initial bool, pat, username, password string) bool {
	// envURL is either set by user or set to match a configURL in a configReload
	if envURL == "" {
		return false
	}
	reset, err := downloadUpdateConfigFile(envURL, existsAlready, initial, pat, username, password)
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
func downloadUpdateConfigFile(urlStr string, existsAlready, initial bool, pat, username, password string) (bool, error) {
	client := http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return false, fmt.Errorf("unable to create request: %v", err)
	}
	if pat != "" {
		req.Header.Add("Authorization", "token "+pat)
		req.Header.Add("Accept", "application/vnd.github.v3+json")
	}
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("config download returned HTTP %d", resp.StatusCode)
	}
	const maxConfigBytes = 1 << 20
	newBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxConfigBytes+1))
	if err != nil {
		return false, fmt.Errorf("error downloading config: %w", err)
	}
	if len(newBytes) > maxConfigBytes {
		return false, fmt.Errorf("downloaded config exceeds 1 MiB")
	}
	if err := validateDownloadedConfig(newBytes); err != nil {
		return false, fmt.Errorf("invalid downloaded config: %w", err)
	}
	var currentConfigBytes []byte
	if !initial {
		currentConfigBytes, err = os.ReadFile(defaultConfigPath)
		if err != nil {
			existsAlready = false
		} else if bytes.Equal(newBytes, currentConfigBytes) {
			return false, nil
		}
	}
	updated, err := replaceConfigFiles(defaultConfigPath, defaultConfigBackup, newBytes, currentConfigBytes, existsAlready && !initial, os.Rename)
	if err != nil {
		return updated, err
	}

	logger.Infof("Config updates found from url: %s, will load new targets", urlStr)
	return true, nil
}

// Validate one nonempty YAML mapping using the same decoder as local configs,
// rejecting unknown fields so an error page cannot masquerade as configuration.
func validateDownloadedConfig(data []byte) error {
	if err := validateSingleConfigMapping(data); err != nil {
		return err
	}
	return decodeConfigExactly(data)
}

func validateSingleConfigMapping(data []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode || len(document.Content[0].Content) == 0 {
		return fmt.Errorf("expected a nonempty configuration mapping")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("expected exactly one YAML document")
	}
	return nil
}

func decodeConfigExactly(data []byte) error {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return err
	}
	var config FetchitConfig
	if err := v.UnmarshalExact(&config); err != nil {
		return err
	}
	return validateLifecycleConfig(&config)
}

// Stage both files before touching either destination. Publish the backup only
// after config replacement succeeds, rolling the config back on backup failure.
func replaceConfigFiles(configPath, backupPath string, next, previous []byte, backup bool, rename func(string, string) error) (bool, error) {
	stage, err := stageConfig(configPath, next)
	if err != nil {
		return false, fmt.Errorf("stage config: %w", err)
	}
	defer os.Remove(stage)
	var backupStage string
	if backup {
		backupStage, err = stageConfig(backupPath, previous)
		if err != nil {
			return false, fmt.Errorf("stage backup: %w", err)
		}
		defer os.Remove(backupStage)
	}
	if err := rename(stage, configPath); err != nil {
		return false, fmt.Errorf("replace config; previous files preserved: %w", err)
	}
	if backup {
		if err := rename(backupStage, backupPath); err != nil {
			if rollbackErr := rename(backupStage, configPath); rollbackErr != nil {
				return true, errors.Join(fmt.Errorf("publish backup: %w", err), fmt.Errorf("rollback failed; new config remains active: %w", rollbackErr))
			}
			return false, fmt.Errorf("publish backup; config rolled back: %w", err)
		}
	}
	return true, nil
}

func stageConfig(destination string, data []byte) (name string, resultErr error) {
	file, err := os.CreateTemp(filepath.Dir(destination), ".fetchit-config-*")
	if err != nil {
		return "", err
	}
	name = file.Name()
	defer func() {
		file.Close()
		if resultErr != nil {
			os.Remove(name)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return name, err
	}
	if err := file.Sync(); err != nil {
		return name, err
	}
	if err := file.Close(); err != nil {
		return name, err
	}
	return name, nil
}
