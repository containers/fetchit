package engine

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

type CommonMethod struct {
	// Name must be unique within target method
	Name string `mapstructure:"name"`
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// A glob to pattern match files in the target path directory
	Glob *string `mapstructure:"glob"`
	// initialRun is set by fetchit
	initialRun bool
	target     *Target
}

func (m *CommonMethod) GetName() string {
	return m.Name
}

func (m *CommonMethod) SchedInfo() SchedInfo {
	return SchedInfo{
		schedule: m.Schedule,
		skew:     m.Skew,
	}
}

func (m *CommonMethod) GetTargetPath() string {
	return m.TargetPath
}

func (m *CommonMethod) GetTarget() *Target {
	return m.target
}

func zeroToCurrent(ctx, conn context.Context, m Method, target *Target, tag *[]string) error {
	current, err := getCurrent(target, m.GetKind(), m.GetName())
	if err != nil {
		return fmt.Errorf("Failed to get current commit: %v", err)
	}

	if current != plumbing.ZeroHash {
		err = m.Apply(ctx, conn, plumbing.ZeroHash, current, tag)
		if err != nil {
			return fmt.Errorf("Failed to apply changes: %v", err)
		}

		klog.Infof("Moved %s to %s for target %s", m.GetName(), current, target.name)
	}

	return nil
}

func currentToLatest(ctx, conn context.Context, m Method, target *Target, tag *[]string) error {
	if target.disconnected && len(target.url) > 0 {
		extractZip(target.url, target.name)
	} else if target.disconnected && len(target.localPath) > 0 {
		localPathPull(target.name, target.localPath)
	} else if target.disconnected && len(target.device) > 0 {
		localDevicePull(target.name, target.device)
	}
	latest, err := getLatest(target)
	if err != nil {
		return fmt.Errorf("Failed to get latest commit: %v", err)
	}

	current, err := getCurrent(target, m.GetKind(), m.GetName())
	if err != nil {
		return fmt.Errorf("Failed to get current commit: %v", err)
	}

	if latest != current {
		err = m.Apply(ctx, conn, current, latest, tag)
		if err != nil {
			return fmt.Errorf("Failed to apply changes: %v", err)
		}

		updateCurrent(ctx, target, latest, m.GetKind(), m.GetName())
		klog.Infof("Moved %s from %s to %s for target %s", m.GetName(), current, latest, target.name)
	} else {
		klog.Infof("No changes applied to target %s this run, %s currently at %s", target.name, m.GetKind(), current)
	}

	return nil
}

func runChanges(ctx context.Context, conn context.Context, m Method, changeMap map[*object.Change]string) error {
	for change, changePath := range changeMap {
		if err := m.MethodEngine(ctx, conn, change, changePath); err != nil {
			return err
		}
	}
	return nil
}
