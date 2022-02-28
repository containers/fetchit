package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

/* below is an example.json file:
{"Image":"docker.io/mmumshad/simple-webapp-color:latest",
"Name": "colors",
"Env": {"color": "blue", "tree": "trunk"},
"Ports": [{
    "HostIP":        "",
    "ContainerPort": 8080,
    "HostPort":      8080,
    "Range":         0,
    "Protocol":      ""}]
}
*/

type Raw struct {
	Image   string                 `json:"Image"`
	Name    string                 `json:"Name"`
	Env     map[string]string      `json:"Env"`
	Ports   []types.PortMapping    `json:"Ports"`
	Mounts  []specs.Mount          `json:"Mounts"`
	Volumes []*specgen.NamedVolume `json:"Volumes"`
}

func RawPodman(path string) error {
	fmt.Printf("Creating podman container from %s\n", path)
	rawJson, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	raw := Raw{Ports: []types.PortMapping{}}
	json.Unmarshal([]byte(rawJson), &raw)
	// Create a new Podman client
	conn, err := bindings.NewConnection(context.Background(), "unix://run/podman/podman.sock")
	if err != nil {
		return err
	}
	_, err = images.Pull(conn, raw.Image, nil)
	if err != nil {
		return err
	}
	inspectData, err := containers.Inspect(conn, raw.Name, new(containers.InspectOptions).WithSize(true))
	if err == nil || inspectData == nil {
		fmt.Printf("A container named %s already exists. Removing the container before redeploy.\n", raw.Name)
		containers.Stop(conn, raw.Name, nil)
		containers.Remove(conn, raw.Name, new(containers.RemoveOptions).WithForce(true))

	}
	// Create a new container
	s := specgen.NewSpecGenerator(raw.Image, false)
	s.Name = raw.Name
	s.Env = map[string]string(raw.Env)
	s.Mounts = []specs.Mount(raw.Mounts)
	s.PortMappings = []types.PortMapping(raw.Ports)
	s.Volumes = []*specgen.NamedVolume(raw.Volumes)
	s.RestartPolicy = "always"
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	fmt.Println("Container created.")
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	fmt.Println("Container started....Requeuing")
	return nil
}
