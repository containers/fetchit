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

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"k8s.io/klog/v2"
)

func extractZip(url, name string) error {
	trimDir := strings.TrimSuffix(url, path.Ext(url))
	directory := filepath.Base(trimDir)
	absPath, err := filepath.Abs(directory)

	klog.Infof("loading disconnected archive from %s", url)
	// Place the data into the placeholder file
	data, err := http.Get(url)
	if err != nil {
		klog.Error("Failed getting data from ", url)
		return err
	}
	defer data.Body.Close()

	// Fail early if http error code is not 200
	if data.StatusCode != http.StatusOK {
		klog.Error("Failed getting data from ", url)
		return err
	}

	// Unzip the data from the http response
	// Create the destination file
	os.MkdirAll(directory, 0755)

	outFile, err := os.Create(absPath + "/" + name + ".zip")
	if err != nil {
		klog.Error("Failed creating file ", absPath+"/"+name+".zip")
		return err
	}

	// Write the body to file
	io.Copy(outFile, data.Body)

	// Unzip the file
	r, err := zip.OpenReader(outFile.Name())
	if err != nil {
		klog.Infof("error opening zip file: %s", err)
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
		klog.Error("Failed removing file ", outFile.Name())
		return err
	}
	return nil
}

func localDevicePull(name, device, trimDir string) (id string, err error) {
	// Need to use the filetransfer method to populate the directory from the localPath
	ctx := context.Background()
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		klog.Error("Failed to create connection to podman")
		return "", err
	}
	// List currently running containers to ensure we don't create a duplicate
	containerName := string(filetransferMethod + "-" + name + "-" + "disconnected" + trimDir)
	klog.Info("Checking for existing container: ", containerName)
	inspectData, err := containers.Inspect(conn, containerName, new(containers.InspectOptions).WithSize(true))
	if err == nil || inspectData == nil {
		klog.Error("The container already exists..requeuing")
		return "", err
	}

	copyFile := ("/mnt/" + name + " " + "/opt" + "/")
	// Set prev	as a nil value to prevent the previous commit from being used
	s := generateDeviceSpec(filetransferMethod, "disconnected"+trimDir, copyFile, device, name)
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return "", err
	}
	// Wait for the container to finish
	waitAndRemoveContainer(conn, createResponse.ID)
	return createResponse.ID, nil
}
