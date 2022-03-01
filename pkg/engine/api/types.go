package api

import (
	"sync"
)

type Repo struct {
	Name   string `mapstructure:"name"`
	PAT    string `mapstructure:"pat"`
	Target Target `mapstructure:"target"`
	Mu     sync.Mutex
}

// TODO Each Target can hold 1 each of Raw, Systemd, FileTransfer
// Or, add a new Target for each in the config file.
type Target struct {
	Url          string       `mapstructure:"url"`
	Branch       string       `mapstructure:"branch"`
	Raw          Raw          `mapstructure:"raw"`
	Systemd      Systemd      `mapstructure:"systemd"`
	Kube         Kube         `mapstructure:"kube"`
	FileTransfer FileTransfer `mapstructure:"fileTransfer"`
}

type Raw struct {
	Subdirectory string `mapstructure:"subdirectory"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
}

type Systemd struct {
	Subdirectory string `mapstructure:"subdirectory"`
	User         string `mapstructure:"user"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
}

type FileTransfer struct {
	Subdirectory string `mapstructure:"subdirectory"`
	Dest         string `mapstructure:"dest"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
}

type Kube struct {
	Subdirectory string `mapstructure:"subdirectory"`
	Schedule     string `mapstructure:"schedule"`
	InitialRun   bool
}
