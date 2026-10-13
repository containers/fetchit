package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
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

func (ft *FileTransfer) Process(ctx, conn context.Context, skew int) {
	target := ft.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	if ft.initialRun {
		err := getRepo(target)
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
			logger.Errorf("Error moving to current: %v target url is: %s ", err, target.url)
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
	targetRoot := filepath.Join(getDirectory(ft.GetTarget()), ft.GetTargetPath())
	var prev *string = nil
	var previousPath string
	if change != nil {
		if change.From.Name != "" {
			prev = &change.From.Name
		}
	}
	if prev != nil {
		relativePath, err := fileTransferRelativePath(filepath.Join(targetRoot, *prev), targetRoot)
		if err != nil {
			return err
		}
		previousPath = relativePath
	}
	if ft.CleanupOnRemoval {
		receipt, _, err := hostMethodReceipt(ft)
		if err != nil {
			return err
		}
		plan := hostArtifactPlan{Receipt: receipt, Action: "apply"}
		if prev != nil {
			plan.Previous = previousPath
		}
		if path == deleteFile {
			plan.Action = "delete"
		} else {
			relativePath, err := fileTransferRelativePath(path, targetRoot)
			if err != nil {
				return err
			}
			plan.Source = filepath.Join("/opt", path)
			plan.DestinationPath = relativePath
		}
		return deployHostArtifact(conn, plan)
	}

	relativePath := ""
	if path != deleteFile {
		var err error
		relativePath, err = fileTransferRelativePath(path, targetRoot)
		if err != nil {
			return err
		}
	}

	dest := ft.DestinationDirectory
	return ft.fileTransferPodman(ctx, conn, path, relativePath, dest, previousPath)
}

func (ft *FileTransfer) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	return ft.applyGitChanges(ctx, conn, ft, currentState, desiredState, tags)
}

func (ft *FileTransfer) fileTransferPodman(ctx, conn context.Context, path, relativePath, dest, previousPath string) error {
	if previousPath != "" {
		pathToRemove := filepath.Join(dest, previousPath)
		s := generateSpecRemove(filetransferMethod, previousPath, pathToRemove, dest, ft.Name)
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

	source := "/opt/" + filepath.ToSlash(path)

	s := generateSpec(filetransferMethod, relativePath, source, dest, ft.Name)
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return err
	}

	// Wait for the container to exit
	return waitAndRemoveContainer(conn, createResponse.ID)
}

func fileTransferRelativePath(path, repository string) (string, error) {
	relativePath, err := filepath.Rel(repository, path)
	if err != nil || relativePath == "" || relativePath == "." || relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file transfer path %q is outside repository %q", path, repository)
	}
	return relativePath, nil
}
