package engine

import (
	"context"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v4/pkg/bindings/system"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const pruneMethod = "prune"

// Prune configures targets to run a podman system prune periodically
type Prune struct {
	CommonMethod `mapstructure:",squash"`
	Volumes      bool `mapstructure:"volumes"`
	All          bool `mapstructure:"all"`
}

func (p *Prune) GetKind() string {
	return pruneMethod
}

func (p *Prune) GetName() string {
	return pruneMethod
}

func (p *Prune) Process(ctx, conn context.Context, PAT string, skew int) {
	target := p.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()
	// Nothing to do with certain file we're just collecting garbage so can call the prunePodman method straight from here
	opts := system.PruneOptions{
		All:     &p.All,
		Volumes: &p.Volumes,
	}

	err := p.prunePodman(ctx, conn, opts)
	if err != nil {
		logger.Debugf("Repository: %s Method: %s encountered error: %v, resetting...", target.url, pruneMethod, err)
	}

}

func (p *Prune) MethodEngine(ctx, conn context.Context, change *object.Change, path string) error {
	return nil
}

func (p *Prune) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	return nil
}

func (p *Prune) prunePodman(ctx, conn context.Context, opts system.PruneOptions) error {
	logger.Info("Pruning system")
	report, err := system.Prune(conn, &opts)
	if err != nil {
		return utils.WrapErr(err, "Error pruning system")
	}
	for _, report := range report.ContainerPruneReports {
		logger.Infof("Pruned container of size %v with id: %s", report.Size, report.Id)
	}

	for _, report := range report.ImagePruneReports {
		logger.Infof("Pruned image of size %v with id: %s", report.Size, report.Id)
	}

	for _, report := range report.PodPruneReport {
		logger.Infof("Pruned pod with id: %s", report.Id)
	}

	for _, report := range report.VolumePruneReports {
		logger.Infof("Pruned volume of size %v with id: %s", report.Size, report.Id)
	}

	logger.Infof("Reclaimed %vB", report.ReclaimedSpace)

	return nil
}
