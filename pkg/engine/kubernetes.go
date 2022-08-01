package engine

import (
	"context"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"

	"k8s.io/klog/v2"
)

const kubernetesMethod = "kubernetes"

// kubernetes to deploy pods from json or yaml files
type Kubernetes struct {
	CommonMethod `mapstructure:",squash"`
}

func (k *Kubernetes) GetKind() string {
	return kubernetesMethod
}

func (k *Kubernetes) Process(ctx context.Context, conn context.Context, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target := k.GetTarget()
	target.mu.Lock()
	defer target.mu.Unlock()

	tag := []string{".yaml", ".yml"}

	if k.initialRun {
		err := getRepo(target, PAT)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.url, target.name, err)
			return
		}

		err = zeroToCurrent(ctx, conn, k, target, &tag)
		if err != nil {
			klog.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, k, target, &tag)
	if err != nil {
		klog.Errorf("Error moving current to latest: %v", err)
		return
	}

	k.initialRun = false
}

func (k *Kubernetes) kubernetesPodman(ctx, conn context.Context, path string, prev *string) error {

	klog.Infof("Deploying Kubernetes assets %s", path)

	return nil
}

func (k *Kubernetes) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	prev, err := getChangeString(change)
	if err != nil {
		return err
	}
	return k.kubernetesPodman(ctx, conn, path, prev)
}

func (k *Kubernetes) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, k.GetTarget(), k.GetTargetPath(), k.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, k, changeMap); err != nil {
		return err
	}
	return nil
}
