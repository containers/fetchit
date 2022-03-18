package engine

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/domain/entities"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/redhat-et/harpoon/pkg/engine/api"

	"k8s.io/klog/v2"
)

const stopped = define.ContainerStateStopped

func fileTransferPodman(ctx context.Context, path, dest, method string, target *api.Target, prev *string) error {

	// Create a new Podman client
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return fmt.Errorf("error podman socket: %w", err)
	}

	if prev != nil {
		pathToRemove := filepath.Join(dest, filepath.Base(*prev))
		s := generateSpecRemove(method, filepath.Base(pathToRemove), pathToRemove, dest, target)
		createResponse, err := createAndStartContainer(conn, s)
		if err != nil {
			return err
		}

		// TODO: check file got removed

		err = waitAndRemoveContainer(conn, createResponse.ID)
		if err != nil {
			return err
		}
	}

	if path == deleteFile {
		return nil
	}

	klog.Infof("Deploying file(s) %s", path)

	file := filepath.Base(path)

	source := filepath.Join("/opt", path)
	copyFile := (source + " " + dest)

	klog.Infof("Identifying if image exists locally")
	// Pull image if it doesn't exist
	err = fetchImage(conn)
	if err != nil {
		return err
	}

	s := generateSpec(method, file, copyFile, dest, target)
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return err
	}

	//TODO: check file got put in the right place

	// Wait for the container to exit
	err = waitAndRemoveContainer(conn, createResponse.ID)

	return err
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

func fetchImage(conn context.Context) error {
	present, err := images.Exists(conn, harpoonImage, nil)
	klog.Infof("Is image present? %t", present)
	if err != nil {
		return err
	}

	if !present {
		_, err = images.Pull(conn, harpoonImage, nil)
		if err != nil {
			return err
		}
	}

	return nil
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
