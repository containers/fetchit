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
	TargetConfigs    []*TargetConfig   `mapstructure:"targetConfigs"`
	ConfigReload     *ConfigReload     `mapstructure:"configReload"`
	Prune            *Prune            `mapstructure:"prune"`
	PodmanAutoUpdate *PodmanAutoUpdate `mapstructure:"podmanAutoUpdate"`
	Images           []*Image          `mapstructure:"images"`
	PAT              string            `mapstructure:"pat"`
	conn             context.Context
	scheduler        *gocron.Scheduler
}

type TargetConfig struct {
	Name         string          `mapstructure:"name"`
	Url          string          `mapstructure:"url"`
	Device       string          `mapstructure:"device"`
	Disconnected bool            `mapstructure:"disconnected"`
	Branch       string          `mapstructure:"branch"`
	Ansible      []*Ansible      `mapstructure:"ansible"`
	FileTransfer []*FileTransfer `mapstructure:"filetransfer"`
	Kubernetes   []*Kubernetes   `mapstructure:"kubernetes"`
	Raw          []*Raw          `mapstructure:"raw"`
	Systemd      []*Systemd      `mapstructure:"systemd"`
	Kube         []*Kube         `mapstructure:"kube"`

	image        *Image
	prune        *Prune
	configReload *ConfigReload
	mu           sync.Mutex
}

type Target struct {
	name         string
	url          string
	device       string
	localPath    string
	branch       string
	mu           sync.Mutex
	disconnected bool
}

type SchedInfo struct {
	schedule string
	skew     *int
}
