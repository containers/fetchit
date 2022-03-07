package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func ansiblePodman(path string) error {
	fmt.Printf("Deploying Ansible playbook %s\n", path)

	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
	}

	copyFile := ("/opt/" + path)
	sshImage := "quay.io/rcook/tools:ansible"

	_, err = images.Pull(conn, sshImage, nil)
	if err != nil {
		fmt.Println(err)
	}

	s := specgen.NewSpecGenerator(sshImage, false)
	s.Name = "ansible"
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// TODO: Remove rcook entries
	s.Command = []string{"sh", "-c", "/usr/bin/ansible-playbook -e ansible_connection=ssh " + copyFile}
	s.Mounts = []specs.Mount{{Source: "/root/.ssh", Destination: "/root/.ssh", Type: "bind", Options: []string{"rw"}}, {Source: "/home/runner/work/harpoon/harpoon/examples/ansible/ansible.cfg", Destination: "/etc/ansible/ansible.cfg", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
	s.NetNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("Container started....Requeuing")
	return nil
}
