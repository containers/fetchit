package engine

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

		logger.Infof("Moved %s to commit %s for git target %s", m.GetName(), current.String()[:hashReportLen], target.url)
	}

	return nil
}

func getDirectory(target *Target) string {
	trimDir := strings.TrimSuffix(target.url, path.Ext(target.url))
	return filepath.Base(trimDir)
}

func currentToLatest(ctx, conn context.Context, m Method, target *Target, tag *[]string) error {
	directory := getDirectory(target)
	if target.disconnected {
		if len(target.url) > 0 {
			extractZip(target.url)
		} else if len(target.device) > 0 {
			localDevicePull(directory, target.device, "", false)
		}
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
		if err := m.Apply(ctx, conn, current, latest, tag); err != nil {
			return fmt.Errorf("Failed to apply changes: %v", err)
		}
		updateCurrent(ctx, target, latest, m.GetKind(), m.GetName())
		logger.Infof("Moved %s from %s to %s for git target %s", m.GetName(), current.String()[:hashReportLen], latest, target.url)
	} else {
		logger.Infof("No changes applied to git target %s this run, %s currently at %s", directory, m.GetKind(), current.String()[:hashReportLen])
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
