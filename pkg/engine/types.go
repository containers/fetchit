package engine

import (
	"context"
	"github.com/go-co-op/gocron"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"sync"
)

type Method interface {
	Type() string
	GetName() string
	Target() *Target
	SchedInfo() SchedInfo
	Process(ctx context.Context, conn context.Context, PAT string, skew int)
	Apply(ctx context.Context, conn context.Context, target *Target, currentState plumbing.Hash, desiredState plumbing.Hash, targetPath string, tags *[]string) error
	MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
}

// FetchitConfig requires necessary objects to process targets
type FetchitConfig struct {
	TargetConfigs []*TargetConfig `mapstructure:"targetConfigs"`
	ConfigTarget  *ConfigTarget   `mapstructure:"configTarget"`
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
	configReload *ConfigTarget
	mu           sync.Mutex
}

type Target struct {
	Name         string
	url          string
	branch       string
	configReload *ConfigTarget
	mu           sync.Mutex
}

type SchedInfo struct {
	schedule string
	skew     *int
}
