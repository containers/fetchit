package engine

import (
	"context"

	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/redhat-et/fetchit/pkg/engine/utils"
	"k8s.io/klog/v2"
)

func cleanPodman(ctx context.Context, conn context.Context, opts system.PruneOptions) error {
	klog.Info("Pruning system")
	report, err := system.Prune(conn, &opts)
	if err != nil {
		return utils.WrapErr(err, "Error pruning system")
	}
	for _, report := range report.ContainerPruneReports {
		klog.Infof("Pruned container of size %v with id: %s\n", report.Size, report.Id)
	}

	for _, report := range report.ImagePruneReports {
		klog.Infof("Pruned image of size %v with id: %s\n", report.Size, report.Id)
	}

	for _, report := range report.PodPruneReport {
		klog.Infof("Pruned pod with id: %s\n", report.Id)
	}

	for _, report := range report.VolumePruneReports {
		klog.Infof("Pruned volume of size %v with id: %s\n", report.Size, report.Id)
	}

	klog.Infof("Reclaimed %vB\n", report.ReclaimedSpace)

	return nil
}
