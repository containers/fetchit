package engine

import (
	"context"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/runtime-spec/specs-go"

	"k8s.io/klog/v2"
)

const ansibleMethod = "ansible"

// Ansible to place and run ansible playbooks
type Ansible struct {
	// Name must be unique within a target
	Name string `mapstructure:"name"`
	// Where in the git repository to fetch a file or directory (to fetch all files in directory)
	TargetPath string `mapstructure:"targetPath"`
	// Schedule is how often to check for git updates with the target files
	// Must be valid cron expression
	Schedule string `mapstructure:"schedule"`
	// Number of seconds to skew the schedule by
	Skew *int `mapstructure:"skew"`
	// SshDirectory for ansible to connect to host
	SshDirectory string `mapstructure:"sshDirectory"`
	// initialRun is set by fetchit
	initialRun bool
	target     *Target
}

func (a *Ansible) Type() string {
	return ansibleMethod
}

func (a *Ansible) GetName() string {
	return a.Name
}

func (a *Ansible) Target() *Target {
	return a.target
}

func (a *Ansible) SchedInfo() SchedInfo {
	return SchedInfo{
		schedule: a.Schedule,
		skew:     a.Skew,
	}
}

func (ans *Ansible) Process(ctx, conn context.Context, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target := ans.Target()
	target.mu.Lock()
	defer target.mu.Unlock()

	tag := []string{"yaml", "yml"}
	if ans.initialRun {
		err := getClone(target, PAT)
		if err != nil {
			klog.Errorf("Failed to clone repo at %s for target %s: %v", target.url, target.Name, err)
			return
		}
	}

	latest, err := getLatest(target)
	if err != nil {
		klog.Errorf("Failed to get latest commit: %v", err)
		return
	}

	current, err := getCurrent(target, ansibleMethod, ans.Name)
	if err != nil {
		klog.Errorf("Failed to get current commit: %v", err)
		return
	}

	if latest != current {
		err = ans.Apply(ctx, conn, target, current, latest, ans.TargetPath, &tag)
		if err != nil {
			klog.Errorf("Failed to apply changes: %v", err)
			return
		}

		updateCurrent(ctx, target, latest, ansibleMethod, ans.Name)
		klog.Infof("Moved ansible %s from %s to %s for target %s", ans.Name, current, latest, target.Name)
	} else {
		klog.Infof("No changes applied to target %s this run, ansible %s currently at %s", target.Name, ans.Name, current)
	}

	ans.initialRun = false
}

func (ans *Ansible) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	return ans.ansiblePodman(ctx, conn, path)
}

func (ans *Ansible) Apply(ctx, conn context.Context, target *Target, currentState, desiredState plumbing.Hash, targetPath string, tags *[]string) error {
	changeMap, err := applyChanges(ctx, target, currentState, desiredState, targetPath, tags)
	if err != nil {
		return err
	}
	if err := ans.runChangesConcurrent(ctx, conn, changeMap); err != nil {
		return err
	}
	return nil
}

func (ans *Ansible) runChangesConcurrent(ctx context.Context, conn context.Context, changeMap map[*object.Change]string) error {
	ch := make(chan error)
	for change, changePath := range changeMap {
		go func(ch chan<- error, changePath string, change *object.Change) {
			if err := ans.MethodEngine(ctx, conn, change, changePath); err != nil {
				ch <- utils.WrapErr(err, "error running engine method for change from: %s to %s", change.From.Name, change.To.Name)
			}
			ch <- nil
		}(ch, changePath, change)
	}
	for range changeMap {
		err := <-ch
		if err != nil {
			return err
		}
	}
	return nil
}

func (ans *Ansible) ansiblePodman(ctx, conn context.Context, path string) error {
	// TODO: add logic to remove
	if path == deleteFile {
		return nil
	}
	klog.Infof("Deploying Ansible playbook %s\n", path)

	copyFile := ("/opt/" + path)
	sshImage := "quay.io/fetchit/fetchit-ansible:latest"

	klog.Infof("Identifying if fetchit-ansible image exists locally")
	if err := detectOrFetchImage(conn, sshImage, true); err != nil {
		return err
	}

	s := specgen.NewSpecGenerator(sshImage, false)
	s.Name = "ansible" + "-" + ans.Name
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// TODO: Remove rcook entries
	s.Command = []string{"sh", "-c", "/usr/bin/ansible-playbook -e ansible_connection=ssh " + copyFile}
	s.Mounts = []specs.Mount{{Source: ans.SshDirectory, Destination: "/root/.ssh", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"ro"}}}
	s.NetNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
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
