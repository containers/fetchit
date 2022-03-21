package engine

import (
	"context"

	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"

	"k8s.io/klog/v2"
)

func ansiblePodman(ctx context.Context, mo *FileMountOptions) error {
	// TODO: add logic to remove
	if mo.Path == deleteFile {
		return nil
	}
	klog.Infof("Deploying Ansible playbook %s\n", mo.Path)

	copyFile := ("/opt/" + mo.Path)
	sshImage := "quay.io/harpoon/harpoon-ansible:latest"

	klog.Infof("Identifying if harpoon-ansible image exists locally")
	// Pull image if it doesn't exist
	var present bool
	var err error
	present, err = images.Exists(mo.Conn, sshImage, nil)
	klog.Infof("Is image present? %t", present)
	if err != nil {
		return err
	}

	if !present {
		_, err = images.Pull(mo.Conn, sshImage, nil)
		if err != nil {
			return err
		}
	}

	s := specgen.NewSpecGenerator(sshImage, false)
	s.Name = "ansible" + "-" + mo.Target.Name
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// TODO: Remove rcook entries
	s.Command = []string{"sh", "-c", "/usr/bin/ansible-playbook -e ansible_connection=ssh " + copyFile}
	s.Mounts = []specs.Mount{{Source: mo.Target.Ansible.SshDirectory, Destination: "/root/.ssh", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
	s.NetNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	createResponse, err := containers.CreateWithSpec(mo.Conn, s, nil)
	if err != nil {
		return err
	}
	klog.Infof("Container created.")
	if err := containers.Start(mo.Conn, createResponse.ID, nil); err != nil {
		return err
	}
	klog.Infof("Container started....Requeuing")
	return nil
}
