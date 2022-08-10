package engine

import (
	"archive/zip"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
)

func extractZip(url string) error {
	trimDir := strings.TrimSuffix(url, path.Ext(url))
	directory := filepath.Base(trimDir)
	cache := "/opt/.cache/" + directory + "/"
	dest := cache + "HEAD"
	absPath, err := filepath.Abs(directory)

	data, err := http.Get(url)
	if err != nil {
		if _, err := os.Stat(dest); err == nil {
			// remove the diff file
			err = os.Remove(dest)
			if err != nil {
				logger.Info("Failed to remove file ", dest)
				return err
			}
		}
		logger.Info("URL not present...requeuing")
		return nil
	} else if data.StatusCode == http.StatusOK {
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			defer data.Body.Close()
			// Check the http response code and if not present exit
			logger.Infof("loading disconnected archive from %s", url)
			// Place the data into the placeholder file

			// Unzip the data from the http response
			// Create the destination file
			os.MkdirAll(directory, 0755)

			outFile, err := os.Create(absPath + "/" + directory + ".zip")
			if err != nil {
				logger.Error("Failed creating file ", absPath+"/"+directory+".zip")
				return err
			}

			// Write the body to file
			io.Copy(outFile, data.Body)

			// Unzip the file
			r, err := zip.OpenReader(outFile.Name())
			if err != nil {
				logger.Infof("error opening zip file: %s", err)
			}
			for _, f := range r.File {
				rc, err := f.Open()
				if err != nil {
					return err
				}
				defer rc.Close()

				fpath := filepath.Join(directory, f.Name)
				if f.FileInfo().IsDir() {
					os.MkdirAll(fpath, f.Mode())
				} else {
					var fdir string
					if lastIndex := strings.LastIndex(fpath, string(os.PathSeparator)); lastIndex > -1 {
						fdir = fpath[:lastIndex]
					}

					os.MkdirAll(fdir, f.Mode())
					f, err := os.OpenFile(
						fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
					if err != nil {
						return err
					}
					defer f.Close()

					_, err = io.Copy(f, rc)
					if err != nil {
						return err
					}
				}
			}
			err = os.Remove(outFile.Name())
			if err != nil {
				logger.Error("Failed removing file ", outFile.Name())
				return err
			}
			createDiffFile(directory)
			return nil
		} else {
			logger.Info("No changes since last disonnected run...requeuing")
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
	if err == nil || inspectData == nil {
		logger.Error("The container already exists..requeuing")
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
		// There's a podman bug somewhere that's causing this
		if err.Error() == "unexpected end of JSON input" {
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
	destination, err := os.Create(dest)
	if err != nil {
		logger.Error("Failed to create file ", dest)
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, srcFile)
	if err != nil {
		logger.Error("Failed to copy file ", src)
		return err
	}
	return nil
}
