package api

import (
	"sync"
)

// TODO Each Target can hold 1 each of Raw, Systemd, FileTransfer
// Or, add a new Target for each in the config file.
type Target struct {
	Name         string       `mapstructure:"name"`
	Url          string       `mapstructure:"url"`
	Branch       string       `mapstructure:"branch"`
	Raw          Raw          `mapstructure:"raw"`
	Systemd      Systemd      `mapstructure:"systemd"`
	Kube         Kube         `mapstructure:"kube"`
	Ansible      Ansible      `mapstructure:"ansible"`
	FileTransfer FileTransfer `mapstructure:"fileTransfer"`
	Mu           sync.Mutex
}

type Raw struct {
	TargetPath string `mapstructure:"targetPath"`
	Schedule   string `mapstructure:"schedule"`
	InitialRun bool
}

type Systemd struct {
	TargetPath string `mapstructure:"targetPath"`
	User       string `mapstructure:"user"`
	Schedule   string `mapstructure:"schedule"`
	InitialRun bool
}

type FileTransfer struct {
	TargetPath string `mapstructure:"targetPath"`
	Dest       string `mapstructure:"dest"`
	Schedule   string `mapstructure:"schedule"`
	InitialRun bool
}

type Kube struct {
	TargetPath string `mapstructure:"targetPath"`
	Schedule   string `mapstructure:"schedule"`
	InitialRun bool
}

type Ansible struct {
	TargetPath   string `mapstructure:"targetPath"`
	SshDirectory string `mapstructure:"sshDirectory"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
}
