package unit

import (
	"testing"

	"github.com/containers/podman/v5/pkg/bindings/images"
)

// TestImagePullOptionsExists tests that Podman v5 image pull options exist
func TestImagePullOptionsExists(t *testing.T) {
	// Test that PullOptions type exists in Podman v5
	opts := &images.PullOptions{}

	if opts == nil {
		t.Fatal("Failed to create PullOptions")
	}
}

// TestImageLoadOptionsExists tests that Podman v5 image load options exist
func TestImageLoadOptionsExists(t *testing.T) {
	// Test that LoadOptions type exists in Podman v5
	opts := &images.LoadOptions{}

	if opts == nil {
		t.Fatal("Failed to create LoadOptions")
	}
}

// TestImageRemoveOptionsExists tests that Podman v5 image remove options exist
func TestImageRemoveOptionsExists(t *testing.T) {
	// Test that RemoveOptions type exists in Podman v5
	opts := &images.RemoveOptions{}

	if opts == nil {
		t.Fatal("Failed to create RemoveOptions")
	}
}

// TestImageListOptionsExists tests that Podman v5 image list options exist
func TestImageListOptionsExists(t *testing.T) {
	// Test that ListOptions type exists in Podman v5
	opts := &images.ListOptions{}

	if opts == nil {
		t.Fatal("Failed to create ListOptions")
	}
}
