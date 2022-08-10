package utils

import (
	"context"

	"github.com/containers/podman/v4/pkg/bindings/images"
)

func FetchImage(conn context.Context, image string) error {
	present, err := images.Exists(conn, image, nil)
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
