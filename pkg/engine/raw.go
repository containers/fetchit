package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"time"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/opencontainers/runtime-spec/specs-go"
	"gopkg.in/yaml.v3"
)

const rawMethod = "raw"

// Raw to deploy pods from json or yaml files
type Raw struct {
	CommonMethod `mapstructure:",squash"`
	// Pull images configured in target files each time regardless of if it already exists
	PullImage bool `mapstructure:"pullImage"`
}

func (r *Raw) GetKind() string {
	return rawMethod
}

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
"CapAdd": []
"CapDrop": []
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
	CapAdd  []string          `json:"CapAdd" yaml:"CapAdd"`
	CapDrop []string          `json:"CapDrop" yaml:"CapDrop"`
}

func (r *Raw) Process(ctx context.Context, conn context.Context, PAT string, skew int) {
	time.Sleep(time.Duration(skew) * time.Millisecond)
	target := r.GetTarget()
	target.mu.Lock()
	defer target.mu.Unlock()

	tag := []string{".json", ".yaml", ".yml"}

	if r.initialRun {
		err := getRepo(target, PAT)
		if err != nil {
			logger.Errorf("Failed to clone repository %s: %v", target.url, err)
			return
		}

		err = zeroToCurrent(ctx, conn, r, target, &tag)
		if err != nil {
			logger.Errorf("Error moving to current: %v", err)
			return
		}
	}

	err := currentToLatest(ctx, conn, r, target, &tag)
	if err != nil {
		logger.Errorf("Error moving current to latest: %v", err)
		return
	}

	r.initialRun = false
}

func (r *Raw) rawPodman(ctx, conn context.Context, path string, prev *string) error {

	logger.Infof("Creating podman container from %s", path)

	rawFile, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	raw, err := rawPodFromBytes(rawFile)
	if err != nil {
		return err
	}

	logger.Infof("Identifying if image exists locally")

	err = detectOrFetchImage(conn, raw.Image, r.PullImage)
	if err != nil {
		return err
	}

	// Delete previous file's podxz
	if prev != nil {
		raw, err := rawPodFromBytes([]byte(*prev))
		if err != nil {
			return err
		}

		err = deleteContainer(conn, raw.Name)
		if err != nil {
			return err
		}

		logger.Infof("Deleted podman container %s", raw.Name)
	}

	if path == deleteFile {
		return nil
	}

	err = removeExisting(conn, raw.Name)
	if err != nil {
		return err
	}

	s := createSpecGen(*raw)

	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	logger.Infof("Container %s created.", s.Name)

	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	logger.Infof("Container %s started....Requeuing", s.Name)

	return nil
}

func (r *Raw) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	prev, err := getChangeString(change)
	if err != nil {
		return err
	}
	return r.rawPodman(ctx, conn, path, prev)
}

func (r *Raw) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	changeMap, err := applyChanges(ctx, r.GetTarget(), r.GetTargetPath(), r.Glob, currentState, desiredState, tags)
	if err != nil {
		return err
	}
	if err := runChanges(ctx, conn, r, changeMap); err != nil {
		return err
	}
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
	s.CapAdd = []string(raw.CapAdd)
	s.CapDrop = []string(raw.CapDrop)
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
		logger.Infof("A container named %s already exists. Removing the container before redeploy.", podName)
		err := deleteContainer(conn, podName)
		if err != nil {
			return err
		}
	}

	return nil
}
