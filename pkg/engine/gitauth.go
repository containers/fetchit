package engine

import (
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
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

// httpBasicAuth leaves anonymous requests without an Authorization header.
func httpBasicAuth(username, password string) transport.AuthMethod {
	if username == "" && password == "" {
		return nil
	}
	return &githttp.BasicAuth{Username: username, Password: password}
}

// Checks to see if private key exists on given path
func checkForPrivateKey(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}
