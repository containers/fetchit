package engine

import (
	"context"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"k8s.io/klog/v2"
)

const cleanMethod = "clean"

// Clean configures targets to run a system prune periodically
type Clean struct {
	CommonMethod `mapstructure:",squash"`
	Volumes      bool `mapstructure:"volumes"`
	All          bool `mapstructure:"all"`
}

func (c *Clean) GetKind() string {
	return cleanMethod
}

func (c *Clean) GetName() string {
	return cleanMethod
}

func (c *Clean) GetTarget() *Target {
	return &Target{
		Name: cleanMethod,
		url:  "",
	}
}

func (c *Clean) SetTarget(t *Target) {
	return
}

func (c *Clean) Process(ctx, conn context.Context, PAT string, skew int) {
	target := c.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()
	// Nothing to do with certain file we're just collecting garbage so can call the cleanPodman method straight from here
	opts := system.PruneOptions{
		All:     &c.All,
		Volumes: &c.Volumes,
	}

	err := c.cleanPodman(ctx, conn, opts)
	if err != nil {
		klog.Warningf("Repo: %s Method: %s encountered error: %v, resetting...", target.Name, cleanMethod, err)
	}

}

func (c *Clean) MethodEngine(ctx, conn context.Context, change *object.Change, path string) error {
	return nil
}

func (c *Clean) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	return nil
}

func (c *Clean) cleanPodman(ctx, conn context.Context, opts system.PruneOptions) error {
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
