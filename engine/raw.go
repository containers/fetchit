package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
)

type Raw struct {
	Image string
	Name  string
	Env   map[string]string
	Ports []types.PortMapping
}

func rawPodman(path string) error {
	fmt.Printf("Creating podman container from %s\n", path)
	rawJson, err := ioutil.ReadFile(path + "/example.json")
	if err != nil {
		return err
	}

	raw := Raw{Ports: []types.PortMapping{}}
	json.Unmarshal([]byte(rawJson), &raw)
	fmt.Printf("%+v\n", raw.Ports)
	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/user/1000/podman/podman.sock")
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
	raw.Ports = append(raw.Ports, types.PortMapping{
		HostIP:        "",
		ContainerPort: 8080,
		HostPort:      8080,
		Range:         0,
		Protocol:      "",
	})
	fmt.Printf("env: %v\n", raw)
	fmt.Printf("ports: %v\n", raw.Ports)
	s := specgen.NewSpecGenerator(raw.Image, false)
	s.Name = raw.Name
	s.Env = map[string]string(raw.Env)
	s.PortMappings = []types.PortMapping(raw.Ports)
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
