package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func systemdPodman(path string) error {
	fmt.Printf("Deploying systemd file(s) %s\n", path)
	systemdFile := filepath.Base(path)

	systemdLocation := "/etc/systemd/system/"
	copyFile := ("/opt/" + path + " " + systemdLocation)

	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		return fmt.Errorf("error podman socket: %w", err)
	}

	s := specgen.NewSpecGenerator(harpoonImage, false)
	s.Name = "systemd" + "-" + systemdFile
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "cp " + copyFile}
	s.Mounts = []specs.Mount{{Source: systemdLocation, Destination: systemdLocation, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	fmt.Println("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	var (
		stopped = define.ContainerStateStopped
	)
	// Wait for the container to exit
	containers.Wait(conn, createResponse.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))

	containers.Remove(conn, createResponse.ID, new(containers.RemoveOptions).WithForce(true))

	fmt.Println("Systemd service started....Requeuing")
	return nil
}
