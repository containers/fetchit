package engine

import (
	"context"
	"path/filepath"

	"k8s.io/klog/v2"
)

func fileTransferPodman(ctx context.Context, mo *SingleMethodObj, path, dest string, prev *string) error {
	if prev != nil {
		pathToRemove := filepath.Join(dest, filepath.Base(*prev))
		s := generateSpecRemove(mo.Method, filepath.Base(pathToRemove), pathToRemove, dest, mo.Target)
		createResponse, err := createAndStartContainer(mo.Conn, s)
		if err != nil {
			return err
		}

		err = waitAndRemoveContainer(mo.Conn, createResponse.ID)
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

	s := generateSpec(mo.Method, file, copyFile, dest, mo.Target)
	createResponse, err := createAndStartContainer(mo.Conn, s)
	if err != nil {
		return err
	}

	// Wait for the container to exit
	return waitAndRemoveContainer(mo.Conn, createResponse.ID)
}
