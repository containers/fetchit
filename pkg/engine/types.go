package engine

import (
	"context"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"sync"
)

type Method interface {
	Type() string
	GetName() string
	SchedInfo() SchedInfo
	Process(ctx context.Context, conn context.Context, target *Target, PAT string, skew int)
	Apply(ctx context.Context, conn context.Context, target *Target, currentState plumbing.Hash, desiredState plumbing.Hash, targetPath string, tags *[]string) error
	MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
}

type Target struct {
	Name            string `mapstructure:"name"`
	Url             string `mapstructure:"url"`
	Branch          string `mapstructure:"branch"`
	Methods         []Method
	Clean           *Clean
	Ansible         []*Ansible      `mapstructure:"ansible"`
	FileTransfer    []*FileTransfer `mapstructure:"filetransfer"`
	Kube            []*Kube         `mapstructure:"kube"`
	Raw             []*Raw          `mapstructure:"raw"`
	Systemd         []*Systemd      `mapstructure:"systemd"`
	ConfigReload    *ConfigTarget
	methodSchedules map[string]SchedInfo
	mu              sync.Mutex
}

type SchedInfo struct {
	Schedule string
	Skew     *int
}
