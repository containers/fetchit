package engine

import (
	"github.com/go-git/go-git/v5/plumbing/object"
	"sync"
)

type Target struct {
	Name            string  `mapstructure:"name"`
	Url             string  `mapstructure:"url"`
	Branch          string  `mapstructure:"branch"`
	Methods         Methods `mapstructure:"methods"`
	methodSchedules map[string]string
	mu              sync.Mutex
}

// Only 1 of each Method per Methods
type Methods struct {
	Raw          *RawTarget          `mapstructure:"raw"`
	Systemd      *SystemdTarget      `mapstructure:"systemd"`
	Kube         *KubeTarget         `mapstructure:"kube"`
	Ansible      *AnsibleTarget      `mapstructure:"ansible"`
	FileTransfer *FileTransferTarget `mapstructure:"fileTransfer"`
	ConfigTarget *ConfigFileTarget   `mapstructure:"configTarget"`
}

// ConfigFileTarget configures a target for dynamic loading of harpoon config updates
// $HARPOON_CONFIG_URL environment variable or a local file with a ConfigFileTarget target
// at ~/.harpoon/config.yaml will inform harpoon to use this target.
// Without this target, harpoon will not watch for config updates.
// At this time, only 1 HarpoonConfigFile target can be passed to harpoon
// TODO: Collect multiple from multiple HarpoonTargets and merge configs into 1 on disk
type ConfigFileTarget struct {
	// Schedule is how often to check for git updates and/or restart the harpoon service
	// Must be valid cron expression
	// With ConfigFileTarget, harpoon will be restarted with each scheduled run
	Schedule string `mapstructure:"schedule"`
	// URL location of config file, such as a raw github URL
	ConfigUrl string `mapstructure:"configUrl"`
	// initialRun is set by harpoon
	initialRun bool
}

// Raw configures target that deploys pods from json or yaml files
type RawTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates to the unit file
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Pull images configured in target files each time regardless of if it already exists
	PullImage bool `mapstructure:"pullImage"`
	// initialRun is set by harpoon
	initialRun bool
	// lastCommit is set by harpoon
	lastCommit *object.Commit
}

// Systemd configures target that places and/or enables systemd unit files
// One unit file per Systemd
// Each URL may have multiple Systemd unit files
type SystemdTarget struct {
	// Where in the git repository to fetch a systemd unit file
	// All '*.service' files will be placed in appropriate systemd path
	// TargetPath must be a single exact file
	TargetPath string `mapstructure:"targetPath"`
	// If true, will place unit file in /etc/systemd/system/
	// If false (default) will place unit file in ~/.config/systemd/user/
	Root bool `mapstructure:"root"`
	// If true, will enable and start all systemd services from fetched unit files
	// If true, will reload and restart the services with every scheduled run
	// Implies Enable=true, will override Enable=false
	Restart bool `mapstructure:"restart"`
	// If true, will enable and start systemd services from fetched unit files
	// If false (default), will place unit file(s) in appropriate systemd path
	Enable bool `mapstructure:"enable"`
	// Schedule is how often to check for git updates to the unit file
	// or how often to restart the services, if Restart=true.
	// If Restart is true, service is restarted on schedule regardless of whether there is git diff
	// This is to, for example, launch with updated image using podman autoupdate,
	// if service runs a podman command
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// initialRun is set by harpoon
	initialRun bool
	// lastCommit is set by harpoon
	lastCommit *object.Commit
}

// FileTransfer configures targets to place files on host system
type FileTransferTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Directory path on the host system in which the target files should be placed
	DestinationDirectory string `mapstructure:"destinationDirectory"`
	// Schedule is how often to check for git updates to the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// initialRun is set by harpoon
	initialRun bool
	// lastCommit is set by harpoon
	lastCommit *object.Commit
}

// Kube configures targets to launch pods using podman kube-play
type KubeTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates with the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// initialRun is set by harpoon
	initialRun bool
	// lastCommit is set by harpoon
	lastCommit *object.Commit
}

// Ansible configures targets to place and run ansible playbooks
type AnsibleTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates with the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// SshDirectory for ansible to connect to host
	SshDirectory string `mapstructure:"sshDirectory"`
	// initialRun is set by harpoon
	initialRun bool
	// lastCommit is set by harpoon
	lastCommit *object.Commit
}
