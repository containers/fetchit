package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/redhat-et/harpoon/pkg/engine/utils"
	"gopkg.in/yaml.v3"

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

type port struct {
	HostIP        string `json:"host_ip" yaml:"host_ip"`
	ContainerPort uint16 `json:"container_port" yaml:"container_port"`
	HostPort      uint16 `json:"host_port" yaml:"host_port"`
	Range         uint16 `json:"range" yaml:"range"`
	Protocol      string `json:"protocol" yaml:"protocol"`
}

type mount struct {
	Destination string   `json:"destination" yaml:"destination"`
	Type        string   `json:"type,omitempty" yaml:"type,omitempty" platform:"linux,solaris,zos"`
	Source      string   `json:"source,omitempty" yaml:"source,omitempty"`
	Options     []string `json:"options,omitempty" yaml:"options,omitempty"`
}

type namedVolume struct {
	Name    string   `json:"name" yaml:"name"`
	Dest    string   `json:"dest" yaml:"dest"`
	Options []string `json:"options" yaml:"options"`
}

type RawPod struct {
	Image   string            `json:"Image" yaml:"Image"`
	Name    string            `json:"Name" yaml:"Name"`
	Env     map[string]string `json:"Env" yaml:"Env"`
	Ports   []port            `json:"Ports" yaml:"Ports"`
	Mounts  []mount           `json:"Mounts" yaml:"Mounts"`
	Volumes []namedVolume     `json:"Volumes" yaml:"Volumes"`
}

func rawPodman(ctx context.Context, mo *FileMountOptions) error {

	// Delete previous file's podxz
	if mo.Previous != nil {
		raw, err := rawPodFromBytes([]byte(*mo.Previous))
		if err != nil {
			return err
		}

		err = deleteContainer(mo.Conn, raw.Name)
		if err != nil {
			return err
		}

		klog.Infof("Deleted podman container %s", raw.Name)
	}

	if mo.Path == deleteFile {
		return nil
	}

	klog.Infof("Creating podman container from %s", mo.Path)

	rawFile, err := ioutil.ReadFile(mo.Path)
	if err != nil {
		return err
	}

	raw, err := rawPodFromBytes(rawFile)
	if err != nil {
		return err
	}

	klog.Infof("Identifying if image exists locally")

	err = detectOrFetchImage(mo.Conn, raw.Image, mo.Target.Raw.PullImage)
	if err != nil {
		return err
	}

	err = removeExisting(mo.Conn, raw.Name)
	if err != nil {
		return err
	}

	s := createSpecGen(*raw)

	createResponse, err := containers.CreateWithSpec(mo.Conn, s, nil)
	if err != nil {
		return err
	}
	klog.Infof("Container %s created.", s.Name)

	if err := containers.Start(mo.Conn, createResponse.ID, nil); err != nil {
		return err
	}
	klog.Infof("Container %s started....Requeuing", s.Name)

	return nil
}

func convertMounts(mounts []mount) []specs.Mount {
	result := []specs.Mount{}
	for _, m := range mounts {
		toAppend := specs.Mount{
			Destination: m.Destination,
			Type:        m.Type,
			Source:      m.Source,
			Options:     m.Options,
		}
		result = append(result, toAppend)
	}
	return result
}

func convertPorts(ports []port) []types.PortMapping {
	result := []types.PortMapping{}
	for _, p := range ports {
		toAppend := types.PortMapping{
			HostIP:        p.HostIP,
			ContainerPort: p.ContainerPort,
			HostPort:      p.HostPort,
			Range:         p.Range,
			Protocol:      p.Protocol,
		}
		result = append(result, toAppend)
	}
	return result
}

func convertVolumes(namedVolumes []namedVolume) []*specgen.NamedVolume {
	result := []*specgen.NamedVolume{}
	for _, n := range namedVolumes {
		toAppend := specgen.NamedVolume{
			Name:    n.Name,
			Dest:    n.Dest,
			Options: n.Options,
		}
		result = append(result, &toAppend)
	}
	return result
}

func createSpecGen(raw RawPod) *specgen.SpecGenerator {
	// Create a new container
	s := specgen.NewSpecGenerator(raw.Image, false)
	s.Name = raw.Name
	s.Env = map[string]string(raw.Env)
	s.Mounts = convertMounts(raw.Mounts)
	s.PortMappings = convertPorts(raw.Ports)
	s.Volumes = convertVolumes(raw.Volumes)
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

func rawPodFromBytes(b []byte) (*RawPod, error) {
	b = bytes.TrimSpace(b)
	raw := RawPod{}
	if b[0] == '{' {
		err := json.Unmarshal(b, &raw)
		if err != nil {
			return nil, utils.WrapErr(err, "Unable to unmarshal json")
		}
	} else {
		err := yaml.Unmarshal(b, &raw)
		if err != nil {
			return nil, utils.WrapErr(err, "Unable to unmarshal yaml")
		}
	}
	return &raw, nil
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
