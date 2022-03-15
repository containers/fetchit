package engine

import (
	"context"
	"encoding/json"
	"io/ioutil"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"

	"k8s.io/klog/v2"
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

type RawPod struct {
	Image   string                 `json:"Image"`
	Name    string                 `json:"Name"`
	Env     map[string]string      `json:"Env"`
	Ports   []types.PortMapping    `json:"Ports"`
	Mounts  []specs.Mount          `json:"Mounts"`
	Volumes []*specgen.NamedVolume `json:"Volumes"`
}

func rawPodman(ctx context.Context, path string, pullImage bool, prev *string) error {

	// Create a new Podman client
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return err
	}

	// Delete previous file's podxz
	if prev != nil {
		raw := rawPodFromBytes([]byte(*prev))

		err := deleteContainer(conn, raw.Name)
		if err != nil {
			return err
		}

		klog.Infof("Deleted podman container %s", raw.Name)
	}

	// Don't continue if no path is set, this means we just have to delete the
	// previous file
	if path == "" {
		return nil
	}

	klog.Infof("Creating podman container from %s", path)

	rawJson, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	raw := rawPodFromBytes(rawJson)

	klog.Infof("Identifying if image exists locally")

	err = detectOrFetchImage(conn, raw.Image, pullImage)
	if err != nil {
		return err
	}

	err = removeExisting(conn, raw.Name)
	if err != nil {
		return err
	}

	s := createSpecGen(raw)

	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	klog.Infof("Container %s created.", s.Name)

	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	klog.Infof("Container %s started....Requeuing", s.Name)

	return nil
}

func createSpecGen(raw RawPod) *specgen.SpecGenerator {
	// Create a new container
	s := specgen.NewSpecGenerator(raw.Image, false)
	s.Name = raw.Name
	s.Env = map[string]string(raw.Env)
	s.Mounts = []specs.Mount(raw.Mounts)
	s.PortMappings = []types.PortMapping(raw.Ports)
	s.Volumes = []*specgen.NamedVolume(raw.Volumes)
	s.RestartPolicy = "always"
	return s
}

func deleteContainer(conn context.Context, podName string) error {
	err := containers.Stop(conn, podName, nil)
	if err != nil {
		return err
	}

	containers.Remove(conn, podName, new(containers.RemoveOptions).WithForce(true))
	if err != nil {
		return err
	}

	return nil
}

func detectOrFetchImage(conn context.Context, imageName string, force bool) error {
	// Pull image if it doesn't exist
	var present bool
	present, err := images.Exists(conn, imageName, nil)
	klog.Infof("Is image present? %t", present)
	if err != nil {
		return err
	}

	if !present || force {
		_, err = images.Pull(conn, imageName, nil)
		if err != nil {
			return err
		}
	}

	return nil
}

func rawPodFromBytes(b []byte) RawPod {
	raw := RawPod{Ports: []types.PortMapping{}}
	json.Unmarshal(b, &raw)
	return raw
}

// Using this might not be necessary
func removeExisting(conn context.Context, podName string) error {
	inspectData, err := containers.Inspect(conn, podName, new(containers.InspectOptions).WithSize(true))
	if err == nil || inspectData == nil {
		klog.Infof("A container named %s already exists. Removing the container before redeploy.", podName)
		err := deleteContainer(conn, podName)
		if err != nil {
			return err
		}
	}

	return nil
}
