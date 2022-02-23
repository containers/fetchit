package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func systemdPodman(path string) error {
	fmt.Printf("Deploying systemd file(s) %s\n", path)
	systemdFile := path[strings.LastIndex(path, "/")+1:]

	systemdLocation := "/etc/systemd/system/"
	copyFile := ("/opt/" + path + " " + "/host" + systemdLocation)

	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
	}

	s := specgen.NewSpecGenerator("quay.io/harpoon/harpoon:latest", false)
	s.Name = "systemd" + "-" + systemdFile
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "cp " + copyFile, "chroot", "/host", "sh", "-c", "systemctl restart " + systemdFile}
	s.Mounts = []specs.Mount{{Source: "/run", Destination: "/run", Type: "bind", Options: []string{"rw"}}, {Source: "/", Destination: "/host", Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: "harpoon-volume", Dest: "/opt", Options: []string{"ro"}}}
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		fmt.Println(err)
	}

	fmt.Println("Systemd service started....Requeuing")
	return nil
}
