package engine

import (
	"context"
	"path/filepath"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/redhat-et/harpoon/pkg/engine/api"

	"k8s.io/klog/v2"
)

const stopped = define.ContainerStateStopped

func fileTransferPodman(ctx context.Context, mo *FileMountOptions, prev *string, dest string) error {
	if prev != nil {
		pathToRemove := filepath.Join(dest, filepath.Base(*prev))
		s := generateSpecRemove(mo.Method, filepath.Base(pathToRemove), pathToRemove, dest, mo.Target)
		createResponse, err := createAndStartContainer(mo.Conn, s)
		if err != nil {
			return err
		}

		err = waitAndRemoveContainer(mo.Conn, createResponse.ID)
		if err != nil {
			return err
		}
	}

	if mo.Path == deleteFile {
		return nil
	}

	klog.Infof("Deploying file(s) %s", mo.Path)

	file := filepath.Base(mo.Path)

	source := filepath.Join("/opt", mo.Path)
	copyFile := (source + " " + dest)

	s := generateSpec(mo.Method, file, copyFile, dest, mo.Target)
	createResponse, err := createAndStartContainer(mo.Conn, s)
	if err != nil {
		return err
	}

	// Wait for the container to exit
	return waitAndRemoveContainer(mo.Conn, createResponse.ID)
}

func generateSpec(method, file, copyFile, dest string, target *api.Target) *specgen.SpecGenerator {
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

func generateSpecRemove(method, file, pathToRemove, dest string, target *api.Target) *specgen.SpecGenerator {
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
