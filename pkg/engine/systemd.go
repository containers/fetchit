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

	"k8s.io/klog/v2"
)

func SystemdPodman(ctx context.Context, path, repoName string) error {
	klog.Infof("Deploying systemd file(s) %s", path)
	systemdFile := filepath.Base(path)

	systemdLocation := "/etc/systemd/system/"
	copyFile := ("/opt/" + path + " " + systemdLocation)

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
	s.Name = "systemd" + "-" + repoName + "-" + systemdFile
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	s.Command = []string{"sh", "-c", "cp " + copyFile}
	s.Mounts = []specs.Mount{{Source: systemdLocation, Destination: systemdLocation, Type: "bind", Options: []string{"rw"}}}
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
	if _, err := os.Stat(copyFile); err == nil {
		klog.Infof("Systemd service unit file(s) %s placed.", systemdFile)
	} else {
		err = fmt.Errorf("Systemd service unit file(s) not in expected location after processing repo: %s: %v", repoName, err)
	}
	// Wait for the container to exit
	containers.Wait(conn, createResponse.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{stopped}))
	containers.Remove(conn, createResponse.ID, new(containers.RemoveOptions).WithForce(true))
	return err
}
