package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func ansiblePodman(path string) error {
	fmt.Printf("Deploying Ansible playbook %s\n", path)
	playbook := filepath.Base(path)

	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
	}

	sshImage := "quay.io/rcook/rcook:ansible"

	_, err = images.Pull(conn, sshImage, nil)
	if err != nil {
		fmt.Println(err)

		s := specgen.NewSpecGenerator(sshImage, false)
		s.Name = "ansible"
		s.Privileged = true
		s.PidNS = specgen.Namespace{
			NSMode: "host",
			Value:  "",
		}
		s.Command = []string{"/usr/bin/ansible-playbook " + playbook}
		s.Mounts = []specs.Mount{{Source: "/home/rcook/.ssh", Destination: "/root/.ssh", Type: "bind", Options: []string{"rw"}}, {Source: "/home/rcook/ansible.cfg", Destination: "/etc/ansible/ansible.cfg", Type: "bind", Options: []string{"rw"}}}
		s.NetNS = specgen.Namespace{
			NSMode: "host",
			Value:  "",
		}
		createResponse, err := containers.CreateWithSpec(conn, s, nil)
		if err != nil {
			fmt.Println(err)
		}
		fmt.Println("Container created.")
		if err := containers.Start(conn, createResponse.ID, nil); err != nil {
			fmt.Println(err)
		}

		fmt.Println("Ansible playbook is running...Requeuing")
		return nil
	}
	return nil
}
