package engine

import (
	"context"
	"io"
	"net/http"
	"os"

	pathPackage "path"

	"github.com/containers/podman/v4/pkg/bindings/images"

	"k8s.io/klog/v2"
)

type ImageLoad struct {
	URL string `json:"URL" yaml:"URL"`
}

func imageLoader(ctx context.Context, conn context.Context, mo *SingleMethodObj) error {

	klog.Infof("Loading image from %s", mo.Target.Methods.Image.Url)
	// Create placeholder file to be populated by the image
	imageName := (pathPackage.Base(mo.Target.Methods.Image.Url))
	localImage, err := os.Create(pathPackage.Join("/tmp", imageName))
	if err != nil {
		klog.Error("Failed creating base file")
		return err
	}
	defer localImage.Close()

	// Place the data into the placeholder file
	data, err := http.Get(mo.Target.Methods.Image.Url)
	if err != nil {
		klog.Error("Failed getting data from ", mo.Target.Methods.Image.Url)
		return err
	}
	defer data.Body.Close()

	// Fail early if http error code is not 200
	if data.StatusCode != http.StatusOK {
		klog.Error("Failed getting data from ", mo.Target.Methods.Image.Url)
		return err
	}

	// Writer the body to file
	_, err = io.Copy(localImage, data.Body)
	if err != nil {
		return err
	}

	// Load the image that is to be imported
	loadableImage, err := os.Open(localImage.Name())
	if err != nil {
		klog.Error("Could not locate image file to load")
	}

	// Load image from path on the system using podman load
	imported, err := images.Load(mo.Conn, loadableImage)
	if err != nil {
		return err
	}

	klog.Infof("Image %s loaded....Requeuing", imported.Names[0])

	// Remove the image file
	err = os.Remove(localImage.Name())
	if err != nil {
		klog.Error("Failed removing image file")
		return err
	}
	return nil
}
