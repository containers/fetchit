package engine

import (
	"os"
	"path/filepath"
)

var defaultSSHKey = filepath.Join("/opt", "mount", ".ssh", "id_rsa")

// Basic type needed for ssh authentication
type GitAuth struct {
	SSH            bool   `mapstructure:"ssh"`
	SSHKeyFile     string `mapstructure:"sshKeyFile"`
	Username       string `mapstructure:"username"`
	Password       string `mapstructure:"password"`
	SecretPassword string `mapstructure:"secret_password"`
	PAT            string `mapstructure:"pat"`
	SecretPAT      string `mapstructure:"secret_pat"`
}

// Checks to see if private key exists on given path
func checkForPrivateKey(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}
