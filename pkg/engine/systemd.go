package engine

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/redhat-et/harpoon/pkg/engine/utils"
	"k8s.io/klog/v2"
)

func systemdPodman(ctx context.Context, mo *FileMountOptions) error {
	klog.Infof("Deploying systemd file(s) %s", mo.Path)
	if err := fileTransferPodman(ctx, mo); err != nil {
		return utils.WrapErr(err, "Error deploying systemd file(s) Repo: %s, Path: %s", mo.Target.Name, mo.Target.Systemd.TargetPath)
	}
	sd := mo.Target.Systemd
	if !sd.Enable {
		klog.Infof("Repo: %s, systemd target successfully processed", mo.Target.Name)
		return nil
	}
	return enableSystemdService(mo.Conn, sd.Root, mo.Dest, filepath.Base(mo.Path), mo.Target.Name)
}

func enableSystemdService(conn context.Context, root bool, systemdPath, service, repoName string) error {
	klog.Infof("Identifying if systemd image exists locally")
	if err := fetchImage(conn, systemdImage); err != nil {
		return err
	}
	os.Setenv("ROOT", "true")
	if !root {
		//os.Setenv("ROOT", "false")
		klog.Info("At this time, harpoon non-root user cannot enable systemd service on the host")
		klog.Infof("To enable this non-root service, run 'systemctl --user enable %s' on host machine", service)
		klog.Info("To enable service as root, run with Systemd.Root = true")
		return nil
	}

	os.Setenv("SERVICE", service)
	s := specgen.NewSpecGenerator(systemdImage, false)
	runMount := "/run/systemd"
	if !root {
		runMount = "/run/user/1000/systemd"
		s.User = "1000"
	}
	s.Name = "systemd-enable-" + service + "-" + repoName
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	envMap := make(map[string]string)
	envMap["ROOT"] = strconv.FormatBool(root)
	envMap["SERVICE"] = service
	envMap["ACTION"] = "enable"
	s.Env = envMap
	s.Mounts = []specs.Mount{{Source: systemdPath, Destination: systemdPath, Type: "bind", Options: []string{"rw"}}, {Source: runMount, Destination: runMount, Type: "bind", Options: []string{"rw"}}}
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return err
	}

	err = waitAndRemoveContainer(conn, createResponse.ID)
	if err != nil {
		return err
	}
	klog.Infof("Repo: %s, systemd target successfully processed", repoName)
	return nil
}
