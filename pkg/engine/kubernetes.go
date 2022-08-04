package engine

import (
	"context"
	"time"

	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/runtime-spec/specs-go"

	"k8s.io/klog/v2"
)

const kubernetesMethod = "Kubernetes"

// Kubernetes to place and run Kubernetes playbooks
type Kubernetes struct {
	CommonMethod `mapstructure:",squash"`
	// Kubeconfig file to be mooved to the container
	Kubeconfig string `mapstructure:"kubeconfig"`
}

func (knetes *Kubernetes) GetKind() string {
	return kubernetesMethod
}

func (knetes *Kubernetes) Process(ctx, conn context.Context, PAT string, skew int) {
	target := knetes.GetTarget()
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target.mu.Lock()
	defer target.mu.Unlock()

	if knetes.initialRun {
		err := getRepo(target, PAT)
		if err != nil {
			if len(target.url) > 0 {
				klog.Errorf("Failed to clone repo at %s for target %s: %v", target.url, target.name, err)
				return
			} else if len(target.localPath) > 0 {
				klog.Errorf("Failed to clone repo at %s for target %s: %v", target.localPath, target.name, err)
				return
			}
		}

		err = zeroToCurrent(ctx, conn, knetes, target, nil)
		if err != nil {
			klog.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, knetes, target, nil)
	if err != nil {
		klog.Errorf("Error moving current to latest: %v", err)
		return
	}

	knetes.initialRun = false
}

func (knetes *Kubernetes) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	return knetes.KubernetesPodman(ctx, conn, path)
}

func (knetes *Kubernetes) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, knetes.GetTarget(), knetes.GetTargetPath(), knetes.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, knetes, changeMap); err != nil {
		return err
	}
	return nil
}

func (knetes *Kubernetes) KubernetesPodman(ctx, conn context.Context, path string) error {
	// TODO: add logic to remove
	if path == deleteFile {
		return nil
	}
	klog.Infof("Deploying Kubernetes object %s\n", path)

	kubectlObject := ("/opt/" + path)
	kubeImage := "docker.io/alpine/k8s:1.21.13"

	klog.Infof("Identifying if fetchit-Kubernetes image exists locally")
	if err := detectOrFetchImage(conn, kubeImage, true); err != nil {
		return err
	}

	s := specgen.NewSpecGenerator(kubeImage, false)
	s.Name = "Kubernetes" + "-" + knetes.Name
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	s.Command = []string{"sh", "-c", "kubectl apply -f " + kubectlObject}
	s.Mounts = []specs.Mount{{Source: knetes.Kubeconfig, Destination: "/root/.kube/config", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"ro"}}}
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	klog.Infof("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	// Wait for the container to exit
	err = waitAndRemoveContainer(conn, createResponse.ID)
	if err != nil {
		return err
	}
	klog.Infof("Container started....Requeuing")
	return nil
}
