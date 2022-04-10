package engine

import (
	"context"
	"os"
	"path/filepath"
	"strconv"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"

	"github.com/redhat-et/harpoon/pkg/engine/utils"
	"k8s.io/klog/v2"
)

func systemdPodman(ctx context.Context, mo *SingleMethodObj, path, dest string, prev *string) error {
	klog.Infof("Deploying systemd file(s) %s", path)
	sd := mo.Target.Methods.Systemd
	if err := fileTransferPodman(ctx, mo, path, dest, prev); err != nil {
		return utils.WrapErr(err, "Error deploying systemd file(s) Target: %s, Path: %s", mo.Target.Name, sd.TargetPath)
	}
	if !sd.Enable {
		klog.Infof("Target: %s, systemd target successfully processed", mo.Target.Name)
		return nil
	}
	return enableSystemdService(mo, "enable", dest, filepath.Base(path))
}

func enableSystemdService(mo *SingleMethodObj, action, dest, service string) error {
	klog.Infof("Target: %s, running systemctl %s %s", mo.Target.Name, action, service)
	sd := mo.Target.Methods.Systemd
	if err := detectOrFetchImage(mo.Conn, systemdImage, true); err != nil {
		return err
	}
	os.Setenv("ROOT", "true")
	if !sd.Root {
		//os.Setenv("ROOT", "false")
		klog.Info("At this time, harpoon non-root user cannot enable systemd service on the host")
		klog.Infof("To enable this non-root service, run 'systemctl --user enable %s' on host machine", service)
		klog.Info("To enable service as root, run with Systemd.Root = true")
		return nil
	}

	s := specgen.NewSpecGenerator(systemdImage, false)
	runMounttmp := "/run"
	runMountsd := "/run/systemd"
	runMountc := "/sys/fs/cgroup"
	if !sd.Root {
		runMountsd = "/run/user/1000/systemd"
		s.User = "1000"
	}
	s.Name = "systemd-" + action + "-" + service + "-" + mo.Target.Name
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	envMap := make(map[string]string)
	envMap["ROOT"] = strconv.FormatBool(sd.Root)
	envMap["SERVICE"] = service
	envMap["ACTION"] = action
	s.Env = envMap
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: define.TypeBind, Options: []string{"rw"}}, {Source: runMounttmp, Destination: runMounttmp, Type: define.TypeTmpfs, Options: []string{"rw"}}, {Source: runMountc, Destination: runMountc, Type: define.TypeBind, Options: []string{"ro"}}, {Source: runMountsd, Destination: runMountsd, Type: define.TypeBind, Options: []string{"rw"}}}
	createResponse, err := createAndStartContainer(mo.Conn, s)
	if err != nil {
		return err
	}

	err = waitAndRemoveContainer(mo.Conn, createResponse.ID)
	if err != nil {
		return err
	}
	klog.Infof("Target: %s, systemd %s %s complete", mo.Target.Name, action, service)
	return nil
}
