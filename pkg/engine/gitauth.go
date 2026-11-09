package engine

import (
	"errors"
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

// Use httpBasicAuth to apply PAT precedence, omit HTTP Basic authentication for
// anonymous requests, and reject incomplete credentials before contacting a Git server.
func httpBasicAuth(target *Target) (transport.AuthMethod, error) {
	username, password := target.username, target.password
	if target.pat != "" {
		username, password = "fetchit", target.pat
	}
	if username == "" && password == "" {
		return nil, nil
	}
	if username == "" || password == "" {
		return nil, errors.New("git username and password must both be provided")
	}
	return &githttp.BasicAuth{Username: username, Password: password}, nil
}

// Checks to see if private key exists on given path
func checkForPrivateKey(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return nil
}
