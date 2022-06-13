package engine

import (
	"context"
	"net/http"
	"time"

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
}

func (i *Image) GetKind() string {
	return imageMethod
}

func (i *Image) SchedInfo() SchedInfo {
	return SchedInfo{
		schedule: i.Schedule,
		skew:     i.Skew,
	}
}

func (i *Image) Process(ctx, conn context.Context, PAT string, skew int) {
	target := i.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	err := i.loadPodman(ctx, conn, i.Url)
	if err != nil {
		klog.Warningf("Repo: %s Method: %s encountered error: %v, resetting...", target.Name, imageMethod, err)
	}

}

func (i *Image) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	return i.loadPodman(ctx, conn, i.Url)
}

func (i *Image) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, i.GetTarget(), i.GetTargetPath(), currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChangesConcurrent(ctx, conn, i, changeMap); err != nil {
		return err
	}
	return nil
}

func (i *Image) loadPodman(ctx, conn context.Context, url string) error {
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
