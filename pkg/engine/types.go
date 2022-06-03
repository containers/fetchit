package engine

import (
	"sync"
)

type Target struct {
	Name            string  `mapstructure:"name"`
	Url             string  `mapstructure:"url"`
	Branch          string  `mapstructure:"branch"`
	Methods         Methods `mapstructure:"methods"`
	methodSchedules map[string]schedInfo
	mu              sync.Mutex
}

// Only 1 of each Method per Methods
type Methods struct {
	Raw          *RawTarget          `mapstructure:"raw"`
	Systemd      *SystemdTarget      `mapstructure:"systemd"`
	Kube         *KubeTarget         `mapstructure:"kube"`
	Ansible      *AnsibleTarget      `mapstructure:"ansible"`
	FileTransfer *FileTransferTarget `mapstructure:"fileTransfer"`
	Clean        *CleanTarget        `mapstructure:"clean"`
	ConfigTarget *ConfigFileTarget   `mapstructure:"configTarget"`
}

// ConfigFileTarget configures a target for dynamic loading of fetchit config updates
// $FETCHIT_CONFIG_URL environment variable or a local file with a ConfigFileTarget target
// at ~/.fetchit/config.yaml will inform fetchit to use this target.
// Without this target, fetchit will not watch for config updates.
// At this time, only 1 FetchitConfigFile target can be passed to fetchit
// TODO: Collect multiple from multiple FetchitTargets and merge configs into 1 on disk
type schedInfo struct {
	Schedule string
	Skew     *int
}

type ConfigFileTarget struct {
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	// With ConfigFileTarget, fetchit will be restarted with each scheduled run
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// URL location of config file, such as a raw github URL
	ConfigUrl string `mapstructure:"configUrl"`
	// initialRun is set by fetchit
	initialRun bool
}

// RawTarget to deploy pods from json or yaml files
type RawTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates to the unit file
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// Pull images configured in target files each time regardless of if it already exists
	PullImage bool `mapstructure:"pullImage"`
	// initialRun is set by fetchit
	initialRun bool
}

// SystemdTarget to place and/or enable systemd unit files on host
type SystemdTarget struct {
	// AutoUpdateAll will start podman-auto-update.service, podman-auto-update.timer
	// on the host. With this field true, all other fields are ignored. To place unit files
	// on host and/or enable individual services, create a separate Target.Methods.Systemd
	// 'podman auto-update' updates all services running podman with the autoupdate label
	// see https://docs.podman.io/en/latest/markdown/podman-auto-update.1.html#systemd-unit-and-timer
	// TODO: update /etc/systemd/system/podman-auto-update.timer.d/override.conf with schedule
	// By default, podman will auto-update at midnight daily when this service is running
	AutoUpdateAll bool `mapstructure:"autoUpdateAll"`
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
	// and/or how often to restart services.
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// initialRun is set by fetchit
	initialRun bool
}

// FileTransferTarget to place files on host system
type FileTransferTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Directory path on the host system in which the target files should be placed
	DestinationDirectory string `mapstructure:"destinationDirectory"`
	// Schedule is how often to check for git updates to the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// initialRun is set by fetchit
	initialRun bool
}

// KubeTarget to launch pods using podman kube-play
type KubeTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates with the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// initialRun is set by fetchit
	initialRun bool
}

// AnsibleTarget to place and run ansible playbooks
type AnsibleTarget struct {
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates with the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// SshDirectory for ansible to connect to host
	SshDirectory string `mapstructure:"sshDirectory"`
	// initialRun is set by fetchit
	initialRun bool
}

// Clean configures targets to run a system prune periodically
type CleanTarget struct {
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	// With ConfigFileTarget, fetchit will be restarted with each scheduled run
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// URL location of config file, such as a raw github URL
	Volumes bool `mapstructure:"volumes"`
	// initialRun is set by fetchit
	All bool `mapstructure:"all"`
}
