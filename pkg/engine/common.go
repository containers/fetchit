package engine

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type CommonMethod struct {
	// Opt-in cleanup of owned workloads when this method leaves configuration.
	CleanupOnRemoval bool `mapstructure:"cleanupOnRemoval"`
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
			return fmt.Errorf("Failed to apply changes: %w", err)
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
			if err := extractZip(target.url); err != nil {
				return fmt.Errorf("refreshing disconnected archive: %w", err)
			}
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
		if err := applyWithRecovery(ctx, conn, m, current, latest, tag); err != nil {
			return fmt.Errorf("Failed to apply changes: %w", err)
		}
		if err := updateCurrent(ctx, target, latest, m.GetKind(), m.GetName()); err != nil {
			return err
		}
		logger.Infof("Moved %s from %s to %s for git target %s", m.GetName(), current.String()[:hashReportLen], latest, target.url)
	} else {
		logger.Infof("No changes applied to git target %s this run, %s currently at %s", directory, m.GetKind(), current.String()[:hashReportLen])
	}

	return nil
}

func runChanges(ctx context.Context, conn context.Context, m Method, changeMap map[*object.Change]string) error {
	changes := make([]*object.Change, 0, len(changeMap))
	for change := range changeMap {
		changes = append(changes, change)
	}
	sort.Slice(changes, func(i, j int) bool {
		return changeName(changes[i], changeMap[changes[i]]) < changeName(changes[j], changeMap[changes[j]])
	})
	for _, change := range changes {
		if err := m.MethodEngine(ctx, conn, change, changeMap[change]); err != nil {
			return err
		}
	}

	return nil
}

func changeName(change *object.Change, path string) string {
	if change != nil {
		if change.To.Name != "" {
			return change.To.Name
		}
		return change.From.Name
	}
	return path
}

func readChangeInput(change *object.Change, path string) ([]byte, error) {
	if path == deleteFile {
		return nil, nil
	}
	if change != nil {
		_, next, err := change.Files()
		if err != nil {
			return nil, err
		}
		if next != nil {
			content, err := next.Contents()
			return []byte(content), err
		}
	}
	return os.ReadFile(path)
}

// processGit serializes methods sharing a target and retries startup replay until
// both the saved revision and latest revision have applied successfully.
func (m *CommonMethod) processGit(ctx, conn context.Context, method Method, skew int, tags *[]string) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target := m.GetTarget()
	target.mu.Lock()
	defer target.mu.Unlock()
	if m.initialRun {
		if err := getRepo(target); err != nil {
			logger.Errorf("Failed to clone repository %s: %v", target.url, err)
			return
		}
		if err := zeroToCurrent(ctx, conn, method, target, tags); err != nil {
			logger.Errorf("Error moving to current: %v", err)
			return
		}
	}
	if err := currentToLatest(ctx, conn, method, target, tags); err != nil {
		logger.Errorf("Error moving current to latest: %v", err)
		return
	}
	m.initialRun = false
}

func (m *CommonMethod) applyGitChanges(ctx, conn context.Context, method Method, current, desired plumbing.Hash, tags *[]string) error {
	changes, err := applyChanges(ctx, m.GetTarget(), m.TargetPath, m.Glob, current, desired, tags)
	if err != nil {
		return err
	}
	return runChanges(ctx, conn, method, changes)
}
