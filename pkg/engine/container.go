package engine

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/containers/common/libnetwork/types"
	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/redhat-et/harpoon/pkg/engine/utils"
	"gopkg.in/yaml.v3"

	"k8s.io/klog/v2"
)

const stopped = define.ContainerStateStopped

func generateSpec(method, file, copyFile, dest string, target *Target) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(harpoonImage, false)
	s.Name = method + "-" + target.Name + "-" + file
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "cp " + copyFile}
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
	return s
}

func generateSpecRemove(method, file, pathToRemove, dest string, target *Target) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(harpoonImage, false)
	s.Name = method + "-" + target.Name + "-" + file
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "rm " + pathToRemove}
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: harpoonVolume, Dest: "/opt", Options: []string{"ro"}}}
	return s
}

func createAndStartContainer(conn context.Context, s *specgen.SpecGenerator) (entities.ContainerCreateResponse, error) {
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return createResponse, err
	}

	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return createResponse, err
	}
	klog.Infof("Container %s created.", s.Name)

	return createResponse, nil
}

func waitAndRemoveContainer(conn context.Context, ID string) error {
	_, err := containers.Wait(conn, ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
	if err != nil {
		return err
	}

	_, err = containers.Remove(conn, ID, new(containers.RemoveOptions).WithForce(true))
	if err != nil {
		// There's a podman bug somewhere that's causing this
		if err.Error() == "unexpected end of JSON input" {
			return nil
		}
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
	present, err := images.Exists(conn, imageName, nil)
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
