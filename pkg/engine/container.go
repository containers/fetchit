package engine

import (
	"context"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const stopped = define.ContainerStateStopped

var (
	truePtr = func(b bool) *bool { return &b }(true)
)

func generateSpec(method, file, copyFile, dest string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	s.Privileged = truePtr
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "rsync -avz" + " " + copyFile}
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"rw"}}}
	return s
}

func generateDeviceSpec(method, file, copyFile, device string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	s.Privileged = truePtr
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "mount" + " " + device + " " + "/mnt/ ; rsync -avz" + " " + copyFile}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"rw"}}}
	s.Devices = []specs.LinuxDevice{{Path: device}}
	return s
}

func generateDevicePresentSpec(method, file, device string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file + "-" + "device-check"
	s.Privileged = truePtr
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "if [ ! -b " + device + " ]; then exit 1; fi"}
	s.Devices = []specs.LinuxDevice{{Path: device}}
	return s
}

func generateSpecRemove(method, file, pathToRemove, dest, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	s.Privileged = truePtr
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "rm " + pathToRemove}
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"ro"}}}
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
