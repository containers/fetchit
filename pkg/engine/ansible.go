package engine

import (
	"context"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"

	"k8s.io/klog/v2"
)

func ansiblePodman(ctx context.Context, path, repoName string, SshDirectory string) error {
	// TODO: add logic to remove
	if path == deleteFile {
		return nil
	}
	klog.Infof("Deploying Ansible playbook %s\n", path)

	// Create a new Podman client
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return err
	}

	copyFile := ("/opt/" + path)
	sshImage := "quay.io/harpoon/harpoon-ansible:latest"

	klog.Infof("Identifying if image exists locally")
	// Pull image if it doesn't exist
	var present bool
	present, err = images.Exists(conn, sshImage, nil)
	klog.Infof("Is image present? %t", present)
	if err != nil {
		return err
	}

	if !present {
		_, err = images.Pull(conn, sshImage, nil)
		if err != nil {
			return err
		}
	}

	s := specgen.NewSpecGenerator(sshImage, false)
	s.Name = "ansible" + "-" + repoName
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// TODO: Remove rcook entries
	s.Command = []string{"sh", "-c", "/usr/bin/ansible-playbook -e ansible_connection=ssh " + copyFile}
	s.Mounts = []specs.Mount{{Source: SshDirectory, Destination: "/root/.ssh", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
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
	klog.Infof("Container started....Requeuing")
	return nil
}
