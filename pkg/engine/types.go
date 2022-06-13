package engine

import (
	"context"
	"sync"

	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Method interface {
	GetName() string
	GetKind() string
	GetTarget() *Target
	Process(ctx context.Context, conn context.Context, PAT string, skew int)
	Apply(ctx context.Context, conn context.Context, currentState plumbing.Hash, desiredState plumbing.Hash, tags *[]string) error
	MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
}

// FetchitConfig requires necessary objects to process targets
type FetchitConfig struct {
	TargetConfigs []*TargetConfig `mapstructure:"targetConfigs"`
	ConfigReload  *ConfigReload   `mapstructure:"configReload"`
	PAT           string          `mapstructure:"pat"`
	volume        string          `mapstructure:"volume"`
	conn          context.Context
	scheduler     *gocron.Scheduler
}

type TargetConfig struct {
	Name         string          `mapstructure:"name"`
	Url          string          `mapstructure:"url"`
	Branch       string          `mapstructure:"branch"`
	Clean        *Clean          `mapstructure:"clean"`
	Ansible      []*Ansible      `mapstructure:"ansible"`
	FileTransfer []*FileTransfer `mapstructure:"filetransfer"`
	Kube         []*Kube         `mapstructure:"kube"`
	Raw          []*Raw          `mapstructure:"raw"`
	Systemd      []*Systemd      `mapstructure:"systemd"`
	configReload *ConfigReload
	mu           sync.Mutex
}

type Target struct {
	Name         string
	url          string
	branch       string
	configReload *ConfigReload
	mu           sync.Mutex
}

type SchedInfo struct {
	schedule string
	skew     *int
}
