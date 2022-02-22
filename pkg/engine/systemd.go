package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

func systemdPodman(path string) error {
	fmt.Printf("Deploying systemd file(s) %s\n", path)
	in, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file: %s\n", err)
	}
	defer in.Close()
	systemdFile := path[strings.LastIndex(path, "/")+1:]
	out, err := os.Create("/etc/systemd/system/" + systemdFile)
	if err != nil {
		fmt.Printf("Error creating file: %s\n", err)
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		fmt.Printf("Error copying file: %s\n", err)
	}
	err = out.Sync()
	if err != nil {
		fmt.Printf("Error syncing file: %s\n", err)
	}
	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	s := specgen.NewSpecGenerator("quay.io/harpoon/harpoon-amd:latest", false)
	s.Name = "systemd"
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"chroot", "/host", "sh", "-c", "systemctl restart " + systemdFile}
	s.Mounts = []specs.Mount{{Source: "/run", Destination: "/run", Type: "bind", Options: []string{"rw"}}, {Source: "/", Destination: "/host", Type: "bind", Options: []string{"ro"}}}
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

	fmt.Println("Systemd service started....Requeuing")
	return nil
}
