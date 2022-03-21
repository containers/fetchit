package engine

import (
	"context"
	"fmt"

	"k8s.io/klog/v2"
)

func systemdPodman(ctx context.Context, mo *FileMountOptions) error {
	klog.Infof("Deploying systemd file(s) %s", mo.Path)
	if err := fileTransferPodman(ctx, mo); err != nil {
		return fmt.Errorf("Repo: %s, Method: %s, %v", err)
	}
	// TODO: Add logic to start services, root/non-root
	klog.Infof("Repo: %s, systemd target successfully processed", mo.Target.Name)
	return nil
}
