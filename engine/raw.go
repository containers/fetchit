package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
)

type Raw struct {
	Image string
	Name  string
}

func rawPodman(path string) error {
	fmt.Printf("Creating podman container from %s\n", path)
	rawJson, err := ioutil.ReadFile(path + "/example.json")
	if err != nil {
		return err
	}

	var raw Raw
	json.Unmarshal([]byte(rawJson), &raw)
	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	_, err = images.Pull(conn, raw.Image, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	inspectData, err := containers.Inspect(conn, raw.Name, new(containers.InspectOptions).WithSize(true))
	if err == nil || inspectData == nil {
		fmt.Printf("A container named %s already exists. Removing the container before redeploy.\n", raw.Name)
		containers.Stop(conn, raw.Name, nil)
		containers.Remove(conn, raw.Name, new(containers.RemoveOptions).WithForce(true))

	}

	s := specgen.NewSpecGenerator(raw.Image, false)
	s.Name = raw.Name
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
	fmt.Println("Container started.")
	return nil
}
