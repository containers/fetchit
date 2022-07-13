package engine

import (
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

const imageMethod = "image"

// Image configures targets to run a system prune periodically
type Image struct {
	CommonMethod `mapstructure:",squash"`
	// Url is the url of the image to be loaded onto the system
	Url string `mapstructure:"url"`
	// ImagePath defines the location of the image to import
	ImagePath string `mapstructure:"imagePath"`
	// Device is the device that the image is stored(USB)
	Device string `mapstructure:"device"`
}

func (i *Image) GetKind() string {
	return imageMethod
}

func (i *Image) Process(ctx, conn context.Context, PAT string, skew int) {
	target := i.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	if len(i.Url) > 0 {
		err := i.loadHTTPPodman(ctx, conn, i.Url, target.name)
		if err != nil {
			klog.Warningf("Repo: %s Method: %s encountered error: %v, resetting...", target.name, imageMethod, err)
		}
	} else if len(i.ImagePath) > 0 {
		err := i.loadDevicePodman(ctx, conn)
		if err != nil {
			klog.Warningf("Repo: %s Method: %s encountered error: %v, resetting...", target.name, imageMethod, err)
		}
	}
}

func (i *Image) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	return nil
}

func (i *Image) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	return nil
}

func (i *Image) loadHTTPPodman(ctx, conn context.Context, url, target string) error {
	klog.Infof("Loading image from %s", i.Url)
	imageName := (path.Base(url))
	// Place the data into the placeholder file
	data, err := http.Get(i.Url)
	if err != nil {
		klog.Error("Failed getting data from ", i.Url)
		return err
	}
	defer data.Body.Close()

	// Fail early if http error code is not 200
	if data.StatusCode != http.StatusOK {
		klog.Error("Failed getting data from ", i.Url)
		return err
	}
	// Create the file to write the data to
	file, err := os.Create("/opt/" + target + imageName)
	if err != nil {
		klog.Error("Failed creating file ", file)
		return err
	}
	// Write the data to the file
	_, err = io.Copy(file, data.Body)
	if err != nil {
		klog.Error("Failed writing data to ", file)
		return err
	}

	pathToLoad := "/opt/" + target + imageName
	err = i.podmanImageLoad(ctx, conn, pathToLoad)
	if err != nil {
		klog.Error("Failed to load image from device")
		return err
	}
	return nil
}

func (i *Image) loadDevicePodman(ctx, conn context.Context) error {
	// Define the path to the image
	trimDir := filepath.Base(i.ImagePath)
	baseDir := filepath.Dir(i.ImagePath)
	pathToLoad := "/opt/" + i.ImagePath
	_, exitCode, err := localDeviceCheck(baseDir, i.Device, "-"+trimDir)
	if err != nil {
		klog.Error("Failed to check device")
		return err
	}
	if exitCode != 0 {
		klog.Info("Device not present...requeuing")
		// List files to see if anything needs to be flushed
		if _, err := os.Stat(pathToLoad); err == nil {
			klog.Info("Flushing image from device ", pathToLoad)
			flushImages(pathToLoad)
		}
		return nil
	} else if exitCode == 0 {
		// If file does not exist pull from the device
		if _, err := os.Stat(pathToLoad); os.IsNotExist(err) {
			id, err := localDevicePull(baseDir, i.Device, "-"+trimDir, true)
			if err != nil {
				klog.Info("Issue pulling image from device ", err)
			}

			// Wait for the image to be copied into the fetchit container
			containers.Wait(conn, id, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
		}
		err = i.podmanImageLoad(ctx, conn, pathToLoad)
		if err != nil {
			klog.Error("Failed to load image", pathToLoad)
			return err
		}
		return nil
	}
	return nil
}

func (i *Image) podmanImageLoad(ctx, conn context.Context, pathToLoad string) error {
	// Load image from path on the system using podman load
	// Read the file that needs to be processed
	klog.Infof("Loading image from %s", i.ImagePath)

	file, err := os.Open(pathToLoad)
	if err != nil {
		klog.Error("Failed opening file ", pathToLoad)
		return err
	}
	defer file.Close()
	imported, err := images.Load(conn, file)
	if err != nil {
		os.Remove(pathToLoad)
		return err
	}

	klog.Infof("Image %s loaded....Requeuing", imported.Names[0])
	return nil
}

func flushImages(imagePath string) {
	if _, err := os.Stat(imagePath); err == nil {
		os.Remove(imagePath)
	}
}
