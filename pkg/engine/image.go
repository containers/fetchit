package engine

import (
	"context"
	"net/http"
	"os"
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
		err := i.loadHTTPPodman(ctx, conn, i.Url)
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

func (i *Image) loadHTTPPodman(ctx, conn context.Context, url string) error {
	klog.Infof("Loading image from %s", i.Url)
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

	// Load image from path on the system using podman load
	imported, err := images.Load(conn, data.Body)
	if err != nil {
		return err
	}

	klog.Infof("Image %s loaded....Requeuing", imported.Names[0])

	return nil
}

func (i *Image) loadDevicePodman(ctx, conn context.Context) error {
	klog.Infof("Loading image from %s", i.ImagePath)
	// Define the path to the image
	trimDir := filepath.Base(i.ImagePath)
	baseDir := filepath.Dir(i.ImagePath)
	id, err := localDevicePull(baseDir, i.Device, trimDir)
	if err != nil {
		klog.Error("Failed to load image from device")
		return err
	}
	// Wait for the image to be copied into the fetchit container
	containers.Wait(conn, id, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
	// Read the file that needs to be processed
	file, err := os.Open("/opt/" + i.ImagePath)
	if err != nil {
		klog.Error("Failed opening file ", i.ImagePath)
		return err
	}
	defer file.Close()
	// Load image from path on the system using podman load
	imported, err := images.Load(conn, file)
	if err != nil {
		os.Remove(i.ImagePath)
		return err
	}

	klog.Infof("Image %s loaded....Requeuing", imported.Names[0])

	os.Remove(i.ImagePath)
	return nil

}
