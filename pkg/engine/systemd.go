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

const (
	podmanAutoUpdateService = "podman-auto-update.service"
	podmanAutoUpdateTimer   = "podman-auto-update.timer"
)

func systemdPodman(ctx context.Context, mo *SingleMethodObj, path, dest string, prev *string) error {
	klog.Infof("Deploying systemd file(s) %s", path)
	sd := mo.Target.Methods.Systemd
	if sd.AutoUpdateAll {
		if !mo.Target.Methods.Systemd.initialRun {
			return nil
		}
		if err := enableRestartSystemdService(mo, "autoupdate", dest, podmanAutoUpdateTimer); err != nil {
			return utils.WrapErr(err, "Error running systemctl enable --now  %s", podmanAutoUpdateTimer)
		}
		return enableRestartSystemdService(mo, "autoupdate", dest, podmanAutoUpdateService)
	}
	if mo.Target.Methods.Systemd.initialRun {
		if err := fileTransferPodman(ctx, mo, path, dest, prev); err != nil {
			return utils.WrapErr(err, "Error deploying systemd file(s) Target: %s, Path: %s", mo.Target.Name, sd.TargetPath)
		}
	}
	if !sd.Enable {
		klog.Infof("Target: %s, systemd target successfully processed", mo.Target.Name)
		return nil
	}
	if (sd.Enable && !sd.Restart) || mo.Target.Methods.Systemd.initialRun {
		if sd.Enable {
			return enableRestartSystemdService(mo, "enable", dest, filepath.Base(path))
		}
	}
	if sd.Restart {
		return enableRestartSystemdService(mo, "restart", dest, filepath.Base(path))
	}
	return nil
}

func enableRestartSystemdService(mo *SingleMethodObj, action, dest, service string) error {
	act := action
	if action == "autoupdate" {
		act = "enable"
	}
	klog.Infof("Target: %s, running systemctl %s %s", mo.Target.Name, act, service)
	sd := mo.Target.Methods.Systemd
	if err := detectOrFetchImage(mo.Conn, systemdImage, false); err != nil {
		return err
	}

	// TODO: remove
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

		s.User = os.Getenv("USER")
		runMountsd = "/run/user/" + s.User + "/systemd"
	}
	s.Privileged = true
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	if action == "autoupdate" {
		s.Mounts = []specs.Mount{{Source: podmanServicePath, Destination: podmanServicePath, Type: define.TypeBind, Options: []string{"rw"}}, {Source: dest, Destination: dest, Type: define.TypeBind, Options: []string{"rw"}}, {Source: runMounttmp, Destination: runMounttmp, Type: define.TypeTmpfs, Options: []string{"rw"}}, {Source: runMountc, Destination: runMountc, Type: define.TypeBind, Options: []string{"ro"}}, {Source: runMountsd, Destination: runMountsd, Type: define.TypeBind, Options: []string{"rw"}}}
	} else {
		s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: define.TypeBind, Options: []string{"rw"}}, {Source: runMounttmp, Destination: runMounttmp, Type: define.TypeTmpfs, Options: []string{"rw"}}, {Source: runMountc, Destination: runMountc, Type: define.TypeBind, Options: []string{"ro"}}, {Source: runMountsd, Destination: runMountsd, Type: define.TypeBind, Options: []string{"rw"}}}
	}
	s.Name = "systemd-" + act + "-" + service + "-" + mo.Target.Name
	envMap := make(map[string]string)
	envMap["ROOT"] = strconv.FormatBool(sd.Root)
	envMap["SERVICE"] = service
	envMap["ACTION"] = act
	s.Env = envMap
	createResponse, err := createAndStartContainer(mo.Conn, s)
	if err != nil {
		return err
	}

	err = waitAndRemoveContainer(mo.Conn, createResponse.ID)
	if err != nil {
		return err
	}
	klog.Infof("Target: %s, systemd %s %s complete", mo.Target.Name, act, service)
	return nil
}
