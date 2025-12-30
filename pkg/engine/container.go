package engine

import (
	"context"
	"strings"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/domain/entities"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const stopped = define.ContainerStateStopped

// validateShellParam validates parameters that will be used in shell commands
// to prevent command injection attacks
func validateShellParam(param string, paramName string) error {
	// Check for shell metacharacters that could enable command injection
	dangerousChars := []string{";", "|", "&", "$", "`", "(", ")", "<", ">", "\n", "\r"}
	for _, char := range dangerousChars {
		if strings.Contains(param, char) {
			return utils.WrapErr(nil, "Invalid %s: contains potentially dangerous character '%s'", paramName, char)
		}
	}
	return nil
}

func generateSpec(method, file, copyFile, dest string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	// Validate parameters to prevent command injection
	if err := validateShellParam(copyFile, "copyFile"); err != nil {
		logger.Errorf("Invalid copyFile parameter: %s", copyFile)
		// Return spec with safe command that will fail
		s.Command = []string{"sh", "-c", "exit 1"}
		return s
	}
	s.Command = []string{"sh", "-c", "rsync -avz" + " " + copyFile}
	s.Mounts = []specs.Mount{{Source: dest, Destination: dest, Type: "bind", Options: []string{"rw"}}}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"rw"}}}
	return s
}

func generateDeviceSpec(method, file, copyFile, device string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	// Validate parameters to prevent command injection
	if err := validateShellParam(copyFile, "copyFile"); err != nil {
		logger.Errorf("Invalid copyFile parameter: %s", copyFile)
		s.Command = []string{"sh", "-c", "exit 1"}
		return s
	}
	if err := validateShellParam(device, "device"); err != nil {
		logger.Errorf("Invalid device parameter: %s", device)
		s.Command = []string{"sh", "-c", "exit 1"}
		return s
	}
	s.Command = []string{"sh", "-c", "mount" + " " + device + " " + "/mnt/ ; rsync -avz" + " " + copyFile}
	s.Volumes = []*specgen.NamedVolume{{Name: fetchitVolume, Dest: "/opt", Options: []string{"rw"}}}
	s.Devices = []specs.LinuxDevice{{Path: device}}
	return s
}

func generateDevicePresentSpec(method, file, device string, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file + "-" + "device-check"
	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	// Validate parameters to prevent command injection
	if err := validateShellParam(device, "device"); err != nil {
		logger.Errorf("Invalid device parameter: %s", device)
		s.Command = []string{"sh", "-c", "exit 1"}
		return s
	}
	s.Command = []string{"sh", "-c", "if [ ! -b " + device + " ]; then exit 1; fi"}
	s.Devices = []specs.LinuxDevice{{Path: device}}
	return s
}

func generateSpecRemove(method, file, pathToRemove, dest, name string) *specgen.SpecGenerator {
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = method + "-" + name + "-" + file
	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}
	// Validate parameters to prevent command injection
	if err := validateShellParam(pathToRemove, "pathToRemove"); err != nil {
		logger.Errorf("Invalid pathToRemove parameter: %s", pathToRemove)
		s.Command = []string{"sh", "-c", "exit 1"}
		return s
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
		// Known Podman v4 bug - log it before suppressing
		// TODO: Verify if this bug still exists in Podman v5.7.0
		if strings.Contains(err.Error(), "unexpected end of JSON input") {
			logger.Errorf("Container removal for %s returned JSON parse error (known Podman v4 bug), container may still be removed. Error: %v", ID, err)
			// Verify container was actually removed
			exists, checkErr := containers.Exists(conn, ID, nil)
			if checkErr == nil && !exists {
				logger.Infof("Verified container %s was successfully removed despite JSON error", ID)
				return nil
			}
			logger.Warnf("Could not verify removal of container %s", ID)
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
