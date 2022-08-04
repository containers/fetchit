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

func (kube *Kubernetes) GetKind() string {
	return kubernetesMethod
}

func (kube *Kubernetes) Process(ctx, conn context.Context, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target := kube.GetTarget()
	target.mu.Lock()
	defer target.mu.Unlock()

	tag := []string{"yaml", "yml"}
	if kube.initialRun {
		err := getRepo(target, PAT)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.url, target.name, err)
			return
		}

		err = zeroToCurrent(ctx, conn, kube, target, &tag)
		if err != nil {
			klog.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, kube, target, &tag)
	if err != nil {
		klog.Errorf("Error moving current to latest: %v", err)
		return
	}

	kube.initialRun = false
}

func (kube *Kubernetes) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	return kube.KubernetesPodman(ctx, conn, path)
}

func (kube *Kubernetes) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, kube.GetTarget(), kube.GetTargetPath(), kube.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, kube, changeMap); err != nil {
		return err
	}
	return nil
}

func (kube *Kubernetes) KubernetesPodman(ctx, conn context.Context, path string) error {
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
	s.Name = "Kubernetes" + "-" + kube.Name
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// TODO: Remove rcook entries
	s.Command = []string{"sh", "-c", "kubectl apply -f " + kubectlObject}
	s.Mounts = []specs.Mount{{Source: kube.Kubeconfig, Destination: "/root/.kube/config", Type: "bind", Options: []string{"rw"}}}
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
