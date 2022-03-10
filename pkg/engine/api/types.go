package api

import (
	"sync"
)

type Target struct {
	Name         string       `mapstructure:"name"`
	Url          string       `mapstructure:"url"`
	Branch       string       `mapstructure:"branch"`
	Raw          Raw          `mapstructure:"raw"`
	Systemd      Systemd      `mapstructure:"systemd"`
	Kube         Kube         `mapstructure:"kube"`
	FileTransfer FileTransfer `mapstructure:"fileTransfer"`
	MethodSchedules map[string]string
	Mu              sync.Mutex
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
