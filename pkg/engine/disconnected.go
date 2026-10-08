package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
)

func extractZip(url string) error {
	trimDir := strings.TrimSuffix(url, path.Ext(url))
	directory := filepath.Base(trimDir)
	dest := "/opt/.cache/" + directory + "/HEAD"
	data, err := http.Get(url)
	if err != nil {
		if _, err := os.Stat(dest); err == nil {
			if err := os.Remove(dest); err != nil {
				return err
			}
		}
		logger.Info("URL not present...requeuing")
		return nil
	}
	defer data.Body.Close()
	if data.StatusCode != http.StatusOK {
		return &HTTPStatusError{Kind: "archive", StatusCode: data.StatusCode}
	}
	if _, err := os.Stat(dest); err == nil {
		logger.Info("No changes since last disconnected run...requeuing")
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	logger.Infof("loading disconnected archive from %s", url)
	// Keep failed downloads outside the repository so they cannot block retries.
	outFile, err := os.CreateTemp(".", ".fetchit-archive-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(outFile.Name())
	if err := copyAndClose(outFile, data.Body); err != nil {
		return err
	}
	r, err := zip.OpenReader(outFile.Name())
	if err != nil {
		return fmt.Errorf("opening downloaded archive: %w", err)
	}
	defer r.Close()
	if err := extractArchive(r.File, directory); err != nil {
		return err
	}
	return createDiffFile(directory)
}

func extractArchive(files []*zip.File, directory string) error {
	// Validate every entry before writing any files, including sibling-prefix escapes.
	for _, f := range files {
		relative, err := filepath.Rel(directory, filepath.Join(directory, f.Name))
		if err != nil || filepath.IsAbs(f.Name) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in ZIP archive: %q", f.Name)
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported ZIP symlink: %q", f.Name)
		}
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, f := range files {
		// os.Root also prevents writes through existing symlinks that escape the root.
		if f.FileInfo().IsDir() {
			if err := root.MkdirAll(f.Name, f.Mode().Perm()); err != nil {
				return err
			}
			continue
		}
		if err := root.MkdirAll(filepath.Dir(f.Name), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := root.OpenFile(f.Name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
		if err != nil {
			rc.Close()
			return err
		}
		writeErr := copyAndClose(out, rc)
		readCloseErr := rc.Close()
		if err := errors.Join(writeErr, readCloseErr); err != nil {
			return err
		}
	}
	return nil
}

func localDevicePull(name, device, trimDir string, image bool) (id string, err error) {
	// Need to use the filetransfer method to populate the directory from the localPath
	ctx := context.Background()
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		logger.Error("Failed to create connection to podman")
		return "", err
	}
	// Ensure that the device is present
	_, exitCode, err := localDeviceCheck(name, device, trimDir)
	if err != nil {
		logger.Error("Failed to check device")
		return "", err
	}
	if exitCode != 0 {
		// remove the diff file
		cache := "/opt/.cache/" + name + "/"
		dest := cache + "/" + "HEAD"
		err = os.Remove(dest)
		logger.Info("Device not present...requeuing")
		return "", nil
	}
	if exitCode == 0 {
		// List currently running containers to ensure we don't create a duplicate
		containerName := string(filetransferMethod + "-" + name + "-" + "disconnected" + "-" + trimDir)
		inspectData, err := containers.Inspect(conn, containerName, new(containers.InspectOptions).WithSize(true))
		if err == nil || inspectData == nil {
			logger.Error("The container already exists..requeuing")
			return "", err
		}

		copyFile := ("/mnt/" + name + " " + "/opt" + "/")
		s := generateDeviceSpec(filetransferMethod, "disconnected"+trimDir, copyFile, device, name)
		createResponse, err := createAndStartContainer(conn, s)
		if err != nil {
			return "", err
		}
		// Wait for the container to finish
		waitAndRemoveContainer(conn, createResponse.ID)
		if !image {
			createDiffFile(name)
		}
		return createResponse.ID, nil
	}
	return "", nil
}

// This function is more of a health check to check if the device is present. If the device
// doesn't exist, it will return an error.
func localDeviceCheck(name, device, trimDir string) (id string, exitcode int32, err error) {
	// Need to use the filetransfer method to populate the directory from the localPath
	ctx := context.Background()
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		logger.Error("Failed to create connection to podman")
		return "", 0, err
	}
	// List currently running containers to ensure we don't create a duplicate
	containerName := string(filetransferMethod + "-" + name + "-" + "disconnected" + trimDir)
	inspectData, err := containers.Inspect(conn, containerName, new(containers.InspectOptions).WithSize(true))
	if err == nil && inspectData != nil {
		logger.Errorf("Container %s already exists, cannot proceed", containerName)
		return "", 0, err
	}

	s := generateDevicePresentSpec(filetransferMethod, "disconnected"+trimDir, device, name)
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return "", 0, err
	}

	// Wait for the container to finish
	exitCode, err := containers.Wait(conn, createResponse.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
	if err != nil {
		return "", exitCode, err
	}

	_, err = containers.Remove(conn, createResponse.ID, new(containers.RemoveOptions).WithForce(true))
	if err != nil {
		// Known Podman v4 bug - log it before suppressing
		// TODO: Verify if this bug still exists in Podman v5.7.0
		if strings.Contains(err.Error(), "unexpected end of JSON input") {
			logger.Errorf("Container removal for %s returned JSON parse error (known Podman v4 bug), container may still be removed. Error: %v", createResponse.ID, err)
			// Verify container was actually removed
			exists, checkErr := containers.Exists(conn, createResponse.ID, nil)
			if checkErr == nil && !exists {
				logger.Infof("Verified container %s was successfully removed despite JSON error", createResponse.ID)
				return "", exitCode, nil
			}
			logger.Warnf("Could not verify removal of container %s", createResponse.ID)
			return "", exitCode, nil
		}
		return "", exitCode, err
	}

	return createResponse.ID, exitCode, nil
}

func createDiffFile(name string) error {
	cache := "/opt/.cache/" + name + "/"
	os.MkdirAll(cache, os.ModePerm)
	// Copy the file to the cache directory
	src := "/opt/" + name + "/" + ".git/logs/HEAD"
	dest := cache + "/" + "HEAD"
	// Read the src file
	srcFile, err := os.Open(src)
	if err != nil {
		logger.Error("Failed to open file ", src)
		return err
	}
	defer srcFile.Close()
	destination, err := os.Create(dest)
	if err != nil {
		logger.Error("Failed to create file ", dest)
		return err
	}
	return copyAndClose(destination, srcFile)
}
