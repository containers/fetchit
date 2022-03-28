package utils

import (
	"context"

	"github.com/containers/podman/v4/pkg/bindings/images"
	"k8s.io/klog/v2"
)

func FetchImage(conn context.Context, image string) error {
	present, err := images.Exists(conn, image, nil)
	klog.Infof("Is image present? %t", present)
	if err != nil {
		return err
	}

	if !present {
		_, err = images.Pull(conn, image, nil)
		if err != nil {
			return err
		}
	}
	return nil
}
