package engine

import (
	"context"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const filetransferMethod = "filetransfer"

// FileTransfer to place files on host system
type FileTransfer struct {
	CommonMethod `mapstructure:",squash"`
	// Directory path on the host system in which the target files should be placed
	DestinationDirectory string `mapstructure:"destinationDirectory"`
}

func (ft *FileTransfer) GetKind() string {
	return filetransferMethod
}

func (ft *FileTransfer) Process(ctx, conn context.Context, PAT string, skew int) {
	target := ft.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	if ft.initialRun {
		err := getRepo(target, PAT)
		if err != nil {
			if len(target.url) > 0 {
				logger.Errorf("Failed to clone repository at %s: %v", target.url, err)
				return
			} else if len(target.localPath) > 0 {
				logger.Errorf("Failed to clone repository %s: %v", target.localPath, err)
				return
			}
		}

		err = zeroToCurrent(ctx, conn, ft, target, nil)
		if err != nil {
			logger.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, ft, target, nil)
	if err != nil {
		logger.Errorf("Error moving current to latest: %v", err)
		return
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

func (ft *FileTransfer) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, ft.GetTarget(), ft.GetTargetPath(), ft.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, ft, changeMap); err != nil {
		return err
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

	logger.Infof("Deploying file(s) %s", path)

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
