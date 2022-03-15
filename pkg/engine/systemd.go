package engine

import (
	"context"
	"fmt"

	"github.com/redhat-et/harpoon/pkg/engine/api"
	"k8s.io/klog/v2"
)

func systemdPodman(ctx context.Context, path, dest string, target *api.Target) error {
	klog.Infof("Deploying systemd file(s) %s", path)
	if err := fileTransferPodman(ctx, path, dest, systemdMethod, target); err != nil {
		return fmt.Errorf("Repo: %s, Method: %s, %v", err)
	}
	// TODO: Add logic to start services, root/non-root
	klog.Infof("Repo: %s, systemd target successfully processed", target.Name)
	return nil
}
