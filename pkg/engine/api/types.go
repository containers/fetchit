package api

import (
	"sync"

	"github.com/go-git/go-git/v5/plumbing/object"
)

type Target struct {
	Name            string       `mapstructure:"name"`
	Url             string       `mapstructure:"url"`
	Branch          string       `mapstructure:"branch"`
	Raw             Raw          `mapstructure:"raw"`
	Systemd         Systemd      `mapstructure:"systemd"`
	Kube            Kube         `mapstructure:"kube"`
	Ansible         Ansible      `mapstructure:"ansible"`
	FileTransfer    FileTransfer `mapstructure:"fileTransfer"`
	MethodSchedules map[string]string
	Mu              sync.Mutex
}

type Raw struct {
	TargetPath string `mapstructure:"targetPath"`
	Schedule   string `mapstructure:"schedule"`
	PullImage  bool   `mapstructure:"pullImage"`
	InitialRun bool
	LastCommit *object.Commit
}

type Systemd struct {
	TargetPath string `mapstructure:"targetPath"`
	Root       bool   `mapstructure:"root"`
	// If non-root, home directory must be supplied to place unit file at $HOME/.config/systemd/user
	// NonRootHomeDir is only required if root: false
	// For non-root, host machine $HOME/.config/systemd/user path must exist
	NonRootHomeDir string `mapstructure:"nonRootHomeDir"`
	Enable         bool   `mapstructure:"enable"`
	Schedule       string `mapstructure:"schedule"`
	InitialRun     bool
	LastCommit     *object.Commit
}

type FileTransfer struct {
	TargetPath           string `mapstructure:"targetPath"`
	DestinationDirectory string `mapstructure:"destinationDirectory"`
	Schedule             string `mapstructure:"schedule"`
	InitialRun           bool
	LastCommit           *object.Commit
}

type Kube struct {
	TargetPath string `mapstructure:"targetPath"`
	Schedule   string `mapstructure:"schedule"`
	InitialRun bool
	LastCommit *object.Commit
}

type Ansible struct {
	TargetPath   string `mapstructure:"targetPath"`
	SshDirectory string `mapstructure:"sshDirectory"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
	LastCommit   *object.Commit
}
