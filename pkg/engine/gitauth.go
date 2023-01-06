package engine

import (
	"os"
	"path/filepath"
)

var defaultSSHKey = filepath.Join("/opt", "mount", ".ssh", "id_rsa")

// Basic type needed for ssh authentication
type GitAuth struct {
	SSH        bool   `mapstructure:"ssh"`
	SSHKeyFile string `mapstructure:"sshKeyFile"`
	Username   string `mapstructure:"username"`
	Password   string `mapstructure:"password"`
	PAT        string `mapstructure:"pat"`
	EnvSecret  string `mapstructure:"envSecret"`
}

// Checks to see if private key exists on given path
func checkForPrivateKey(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}
