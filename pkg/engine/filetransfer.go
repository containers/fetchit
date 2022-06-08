package engine

import (
	"context"
	"path/filepath"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

const filetransferMethod = "filetransfer"

// FileTransfer to place files on host system
type FileTransfer struct {
	// Name must be unique within target FileTransfer methods
	Name string `mapstructure:"name"`
	// Schedule is how often to check for git updates and/or restart the fetchit service
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Directory path on the host system in which the target files should be placed
	DestinationDirectory string `mapstructure:"destinationDirectory"`
	// initialRun is set by fetchit
	initialRun bool
	target     *Target
}

func (ft *FileTransfer) Type() string {
	return filetransferMethod
}

func (ft *FileTransfer) GetName() string {
	return ft.Name
}

func (ft *FileTransfer) SchedInfo() SchedInfo {
	return SchedInfo{
		schedule: ft.Schedule,
		skew:     ft.Skew,
	}
}

func (ft *FileTransfer) Target() *Target {
	return ft.target
}

func (ft *FileTransfer) Process(ctx, conn context.Context, PAT string, skew int) {
	target := ft.Target()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	if ft.initialRun {
		err := getClone(target, PAT)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.url, target.Name, err)
			return
		}
	}

	latest, err := getLatest(target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := getCurrent(target, filetransferMethod, ft.Name)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = ft.Apply(ctx, conn, target, current, latest, ft.TargetPath, nil)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		updateCurrent(ctx, target, latest, filetransferMethod, ft.Name)
		klog.Infof("Moved filetransfer %s from %s to %s for target %s", ft.Name, current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, filetransfer currently at %s", target.Name, current)
	}

	ft.initialRun = false
}

func (ft *FileTransfer) MethodEngine(ctx, conn context.Context, change *object.Change, path string) error {
	var prev *string = nil
	if change != nil {
		if change.To.Name != "" {
			prev = &change.To.Name
		}
	}
	dest := ft.DestinationDirectory
	return ft.fileTransferPodman(ctx, conn, path, dest, prev)
}

func (ft *FileTransfer) Apply(ctx, conn context.Context, target *Target, currentState, desiredState plumbing.Hash, targetPath string, tags *[]string) error {
	changeMap, err := applyChanges(ctx, target, currentState, desiredState, targetPath, tags)
	if err != nil {
		return err
	}
	if err := ft.runChangesConcurrent(ctx, conn, changeMap); err != nil {
		return err
	}
	return nil
}

func (ft *FileTransfer) runChangesConcurrent(ctx context.Context, conn context.Context, changeMap map[*object.Change]string) error {
	ch := make(chan error)
	for change, changePath := range changeMap {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := ft.MethodEngine(ctx, conn, change, changePath); err != nil {
				ch <- utils.WrapErr(err, "error running engine method for change from: %s to %s", change.From.Name, change.To.Name)
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changeMap {
		err := <-ch
		if err != nil {
			return err
		}
	}
	return nil
}

func (ft *FileTransfer) fileTransferPodman(ctx, conn context.Context, path, dest string, prev *string) error {
	if prev != nil {
		pathToRemove := filepath.Join(dest, filepath.Base(*prev))
		s := generateSpecRemove(filetransferMethod, filepath.Base(pathToRemove), pathToRemove, dest, ft.Name)
		createResponse, err := createAndStartContainer(conn, s)
		if err != nil {
			return err
		}

		err = waitAndRemoveContainer(conn, createResponse.ID)
		if err != nil {
			return err
		}
	}

	if path == deleteFile {
		return nil
	}

	klog.Infof("Deploying file(s) %s", path)

	file := filepath.Base(path)

	source := filepath.Join("/opt", path)
	copyFile := (source + " " + dest)

	s := generateSpec(filetransferMethod, file, copyFile, dest, ft.Name)
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return err
	}

	// Wait for the container to exit
	return waitAndRemoveContainer(conn, createResponse.ID)
}
