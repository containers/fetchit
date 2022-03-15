package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/redhat-et/harpoon/pkg/engine/api"

	"k8s.io/klog/v2"
)

func fileTransferPodman(ctx context.Context, path, dest, method string, target *api.Target) error {
	klog.Infof("Deploying file(s) %s", path)
	file := filepath.Base(path)

	source := filepath.Join("/opt", path)
	copyFile := (source + " " + dest)

	// Create a new Podman client
	conn, err := bindings.NewConnection(ctx, "unix://run/podman/podman.sock")
	if err != nil {
		return fmt.Errorf("error podman socket: %w", err)
	}

	klog.Infof("Identifying if image exists locally")
	// Pull image if it doesn't exist
	var present bool
	present, err = images.Exists(conn, harpoonImage, nil)
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
	createResponse, err := containers.CreateWithSpec(conn, s, nil)
	if err != nil {
		return err
	}
	if err := containers.Start(conn, createResponse.ID, nil); err != nil {
		return err
	}
	klog.Infof("Container %s created.", s.Name)
	var (
		stopped = define.ContainerStateStopped
	)

	destFile := filepath.Join(dest, file)
	if _, err := os.Stat(destFile); err == nil {
		klog.Infof("Transferred file(s) %s successfully.", file)
	} else {
		err = fmt.Errorf("Transferred file %s not in expected location after processing target: %s: %v", destFile, target.Name, err)
	}
	// Wait for the container to exit
	containers.Wait(conn, createResponse.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
	containers.Remove(conn, createResponse.ID, new(containers.RemoveOptions).WithForce(true))
	return err
}
