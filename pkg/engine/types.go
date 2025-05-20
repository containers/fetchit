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
	Process(ctx context.Context, conn context.Context, skew int)
	Apply(ctx context.Context, conn context.Context, currentState plumbing.Hash, desiredState plumbing.Hash, tags *[]string) error
	MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
}

// FetchitConfig requires necessary objects to process targets
type FetchitConfig struct {
	GitAuth          *GitAuth          `mapstructure:"gitAuth"`
	TargetConfigs    []*TargetConfig   `mapstructure:"targetConfigs"`
	ConfigReload     *ConfigReload     `mapstructure:"configReload"`
	Prune            *Prune            `mapstructure:"prune"`
	PodmanAutoUpdate *PodmanAutoUpdate `mapstructure:"podmanAutoUpdate"`
	Images           []*Image          `mapstructure:"images"`
	conn             context.Context
	scheduler        *gocron.Scheduler
}

type TargetConfig struct {
	Name              string             `mapstructure:"name"`
	Url               string             `mapstructure:"url"`
	Device            string             `mapstructure:"device"`
	Disconnected      bool               `mapstructure:"disconnected"`
	VerifyCommitsInfo *VerifyCommitsInfo `mapstructure:"verifyCommitsInfo"`
	Branch            string             `mapstructure:"branch"`
	Ansible           []*Ansible         `mapstructure:"ansible"`
	FileTransfer      []*FileTransfer    `mapstructure:"filetransfer"`
	Kube              []*Kube            `mapstructure:"kube"`
	Raw               []*Raw             `mapstructure:"raw"`
	Systemd           []*Systemd         `mapstructure:"systemd"`

	image        *Image
	prune        *Prune
	configReload *ConfigReload
	mu           sync.Mutex
}

type Target struct {
	ssh             bool
	sshKey          string
	url             string
	pat             string
	envSecret       string
	username        string
	password        string
	device          string
	localPath       string
	branch          string
	mu              sync.Mutex
	disconnected    bool
	// Verification functionality is disabled in this build
	// TODO: Re-enable when compatibility issues are resolved
	//gitsignVerify   bool
	//gitsignRekorURL string
}

type SchedInfo struct {
	schedule string
	skew     *int
}

type VerifyCommitsInfo struct {
	// Verification functionality is disabled in this build
	// TODO: Re-enable when compatibility issues are resolved
	GitsignVerify bool `json:"-"`
	GitsignRekorURL string `json:"-"`
}
