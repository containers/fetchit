package engine

import (
	"context"
	"io"
	"net/http"
	"os"

	"github.com/containers/podman/v4/pkg/bindings/images"

	"k8s.io/klog/v2"
)

func imageLoad(ctx context.Context, conn context.Context, path string) error {

	klog.Infof("Loading image from %s", path)
	// Create placeholder
	// TODO: FIX IMAGE NAME TO BE UNIQUE
	imageFile, err := os.Create("/tmp/image.tar")
	if err != nil {
		klog.Error("Failed creating base file")
		return err
	}
	defer imageFile.Close()

	// Place the data into the placeholder file
	data, err := http.Get(path)
	if err != nil {
		klog.Error("Failed getting data from %s", path)
		return err
	}
	defer data.Body.Close()

	// Fail early if http error code is not 200
	if data.StatusCode != http.StatusOK {
		klog.Error("Failed getting data from %s", path)
		return err
	}

	// Writer the body to file
	_, err = io.Copy(imageFile, data.Body)
	if err != nil {
		return err
	}

	// Load the image that is to be imported
	loadableImage, err := os.Open(imageFile.Name())
	if err != nil {
		klog.Error("Could not locate image file to load")
	}

	// Load image from path on the system using podman load
	load, err := images.Load(conn, loadableImage)
	if err != nil {
		klog.Error("Could not load image")
	}

	klog.Infof("Image %s loaded....Requeuing", load.Names[0])
	defer loadableImage.Close()

	return nil
}
