// Package engine provides deployment methods for fetchit
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/containers/fetchit/pkg/engine/utils"
	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/specgen"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

const quadletMethod = "quadlet"

// QuadletFileType represents the type of Quadlet file
type QuadletFileType string

const (
	QuadletContainer QuadletFileType = "container"
	QuadletVolume    QuadletFileType = "volume"
	QuadletNetwork   QuadletFileType = "network"
	QuadletKube      QuadletFileType = "kube"
)

// QuadletDirectoryPaths holds the directory configuration for Quadlet deployments
type QuadletDirectoryPaths struct {
	// InputDirectory is where Quadlet files are placed for systemd generator
	// Rootful: /etc/containers/systemd/
	// Rootless: ~/.config/containers/systemd/
	InputDirectory string

	// XDGRuntimeDir is the runtime directory for rootless mode
	// Only set for rootless deployments
	// Typically /run/user/<UID>
	XDGRuntimeDir string

	// HomeDirectory is the user's home directory
	// Required for rootless deployments to construct paths
	HomeDirectory string
}

// QuadletFileMetadata represents metadata about a Quadlet file being deployed
type QuadletFileMetadata struct {
	// SourcePath is the path in the Git repository
	SourcePath string

	// TargetPath is the destination path in the Quadlet directory
	// Rootful: /etc/containers/systemd/<filename>
	// Rootless: ~/.config/containers/systemd/<filename>
	TargetPath string

	// FileType indicates the type of Quadlet file
	FileType QuadletFileType

	// ServiceName is the generated systemd service name
	// e.g., "myapp.container" -> "myapp.service"
	ServiceName string

	// ChangeType indicates what operation is being performed
	ChangeType string
}

// Quadlet implements the Method interface for Podman Quadlet deployments
type Quadlet struct {
	CommonMethod `mapstructure:",squash"`

	// Root indicates whether to deploy in rootful (true) or rootless (false) mode
	// Rootful: Files placed in /etc/containers/systemd/ (requires root)
	// Rootless: Files placed in ~/.config/containers/systemd/ (user-level)
	Root bool `mapstructure:"root"`

	// Enable indicates whether to enable and start systemd services after deployment
	// If false, Quadlet files are placed but services are not enabled
	Enable bool `mapstructure:"enable"`

	// Restart indicates whether to restart services on each update
	// If true, implies Enable=true
	// If false and Enable=true, services are enabled but not restarted on updates
	Restart bool `mapstructure:"restart"`

	// initialRun tracks if this is the first execution for this target
	// Used to determine whether to perform initial clone or just fetch updates
	initialRun bool
}

// GetKind returns the method type identifier
func (q *Quadlet) GetKind() string {
	return "quadlet"
}

// GetQuadletDirectory returns the appropriate directory based on mode
func GetQuadletDirectory(root bool) (QuadletDirectoryPaths, error) {
	if root {
		// Rootful deployment
		return QuadletDirectoryPaths{
			InputDirectory: "/etc/containers/systemd",
		}, nil
	}

	// Rootless deployment
	homeDir := os.Getenv("HOME")
	if homeDir == "" {
		return QuadletDirectoryPaths{}, fmt.Errorf("HOME environment variable not set (required for rootless)")
	}

	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfigHome == "" {
		xdgConfigHome = filepath.Join(homeDir, ".config")
	}

	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		uid := os.Getuid()
		xdgRuntimeDir = fmt.Sprintf("/run/user/%d", uid)
	}

	return QuadletDirectoryPaths{
		InputDirectory: filepath.Join(xdgConfigHome, "containers", "systemd"),
		XDGRuntimeDir:  xdgRuntimeDir,
		HomeDirectory:  homeDir,
	}, nil
}

// ensureQuadletDirectory creates the Quadlet directory on the HOST filesystem using a temporary container
// This is necessary because the fetchit container cannot create directories on the host directly
func (q *Quadlet) ensureQuadletDirectory(conn context.Context) error {
	paths, err := GetQuadletDirectory(q.Root)
	if err != nil {
		return fmt.Errorf("failed to get Quadlet directory: %w", err)
	}

	// Create a temporary container to create the directory on the host
	s := specgen.NewSpecGenerator(fetchitImage, false)
	s.Name = "quadlet-mkdir-" + q.Name
	privileged := true
	s.Privileged = &privileged

	// Determine bind mount point and directory creation command
	var mountSource, mountDest string
	if q.Root {
		// Rootful: bind mount /etc to create /etc/containers/systemd
		mountSource = "/etc"
		mountDest = "/etc"
	} else {
		// Rootless: bind mount $HOME to create ~/.config/containers/systemd
		mountSource = paths.HomeDirectory
		mountDest = paths.HomeDirectory
	}

	// Command to create the directory with proper permissions using mkdir -p
	s.Command = []string{"sh", "-c", "mkdir -p " + paths.InputDirectory}

	// Bind mount the base directory so we can create the full path
	s.Mounts = []specs.Mount{{Source: mountSource, Destination: mountDest, Type: "bind", Options: []string{"rw"}}}

	// Create and start the container
	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		return fmt.Errorf("failed to create directory container: %w", err)
	}

	// Wait for the container to exit and remove it
	return waitAndRemoveContainer(conn, createResponse.ID)
}

// runSystemctlCommand runs a systemctl command via a temporary container
func runSystemctlCommand(conn context.Context, root bool, action, service string) error {
	mode := "rootful"
	if !root {
		mode = "rootless"
	}
	logger.Infof("[QUADLET DEBUG] Running systemctl command: action=%s, service=%s, mode=%s", action, service, mode)

	if err := detectOrFetchImage(conn, systemdImage, false); err != nil {
		return err
	}

	s := specgen.NewSpecGenerator(systemdImage, false)
	runMounttmp := "/run"
	runMountsd := "/run/systemd"
	runMountc := "/sys/fs/cgroup"
	xdg := ""

	// Get Quadlet directory to mount
	quadletPaths, err := GetQuadletDirectory(root)
	if err != nil {
		return fmt.Errorf("failed to get Quadlet directory: %w", err)
	}
	quadletDir := quadletPaths.InputDirectory
	logger.Infof("[QUADLET DEBUG] Quadlet directory: %s", quadletDir)

	if !root {
		// Rootless mode - use user's XDG_RUNTIME_DIR
		xdg = os.Getenv("XDG_RUNTIME_DIR")
		if xdg == "" {
			uid := os.Getuid()
			xdg = fmt.Sprintf("/run/user/%d", uid)
			logger.Infof("[QUADLET DEBUG] XDG_RUNTIME_DIR not set, using default: %s", xdg)
		} else {
			logger.Infof("[QUADLET DEBUG] XDG_RUNTIME_DIR: %s", xdg)
		}
		runMountsd = filepath.Join(xdg, "systemd")
		runMounttmp = xdg
	}

	privileged := true
	s.Privileged = &privileged
	s.PidNS = specgen.Namespace{
		NSMode: "host",
		Value:  "",
	}

	// Mount systemd directories AND Quadlet directory
	s.Mounts = []specs.Mount{
		{Source: quadletDir, Destination: quadletDir, Type: define.TypeBind, Options: []string{"rw"}},
		{Source: runMounttmp, Destination: runMounttmp, Type: define.TypeTmpfs, Options: []string{"rw"}},
		{Source: runMountc, Destination: runMountc, Type: define.TypeBind, Options: []string{"ro"}},
		{Source: runMountsd, Destination: runMountsd, Type: define.TypeBind, Options: []string{"rw"}},
	}

	s.Name = "quadlet-systemctl-" + action + "-" + service
	envMap := make(map[string]string)
	envMap["ROOT"] = strconv.FormatBool(root)
	envMap["SERVICE"] = service
	envMap["ACTION"] = action
	envMap["HOME"] = os.Getenv("HOME")
	if !root {
		envMap["XDG_RUNTIME_DIR"] = xdg
	}
	s.Env = envMap

	logger.Infof("[QUADLET DEBUG] Container env: ROOT=%s, SERVICE=%s, ACTION=%s, HOME=%s, XDG_RUNTIME_DIR=%s",
		envMap["ROOT"], envMap["SERVICE"], envMap["ACTION"], envMap["HOME"], envMap["XDG_RUNTIME_DIR"])
	logger.Infof("[QUADLET DEBUG] Container mounts: quadlet=%s, tmpfs=%s, cgroup=%s, systemd=%s",
		quadletDir, runMounttmp, runMountc, runMountsd)

	createResponse, err := createAndStartContainer(conn, s)
	if err != nil {
		logger.Errorf("[QUADLET DEBUG] Failed to create container: %v", err)
		return utils.WrapErr(err, "Failed to run systemctl %s %s", action, service)
	}

	logger.Infof("[QUADLET DEBUG] Container created: %s", createResponse.ID)

	// Wait for container to finish
	_, waitErr := containers.Wait(conn, createResponse.ID, new(containers.WaitOptions).WithCondition([]define.ContainerStatus{define.ContainerStateStopped, define.ContainerStateExited}))
	if waitErr != nil {
		logger.Errorf("[QUADLET DEBUG] Error waiting for container: %v", waitErr)
	}

	// Get container logs before removing
	logOptions := new(containers.LogOptions).WithStdout(true).WithStderr(true)
	stdoutChan := make(chan string, 100)
	stderrChan := make(chan string, 100)

	// Start goroutine to collect logs
	go func() {
		logErr := containers.Logs(conn, createResponse.ID, logOptions, stdoutChan, stderrChan)
		if logErr != nil {
			logger.Errorf("[QUADLET DEBUG] Failed to get container logs: %v", logErr)
		}
	}()

	// Read logs from both channels
	logger.Infof("[QUADLET DEBUG] Container %s output:", createResponse.ID)
	for {
		select {
		case line, ok := <-stdoutChan:
			if !ok {
				stdoutChan = nil
			} else {
				logger.Infof("[CONTAINER STDOUT] %s", line)
			}
		case line, ok := <-stderrChan:
			if !ok {
				stderrChan = nil
			} else {
				logger.Infof("[CONTAINER STDERR] %s", line)
			}
		}
		if stdoutChan == nil && stderrChan == nil {
			break
		}
	}

	// Check exit code
	inspectData, inspectErr := containers.Inspect(conn, createResponse.ID, new(containers.InspectOptions))
	if inspectErr == nil {
		exitCode := inspectData.State.ExitCode
		logger.Infof("[QUADLET DEBUG] Container exit code: %d", exitCode)
		if exitCode != 0 {
			logger.Errorf("[QUADLET DEBUG] Container exited with non-zero code: %d", exitCode)
		}
	}

	// Remove container
	_, removeErr := containers.Remove(conn, createResponse.ID, new(containers.RemoveOptions).WithForce(true))
	if removeErr != nil {
		logger.Warnf("[QUADLET DEBUG] Failed to remove container: %v", removeErr)
	}

	// Return error if container failed
	if inspectErr == nil && inspectData.State.ExitCode != 0 {
		return fmt.Errorf("systemctl container exited with code %d", inspectData.State.ExitCode)
	}

	logger.Infof("[QUADLET DEBUG] Container %s completed successfully", createResponse.ID)
	return nil
}

// systemdDaemonReload triggers systemd to reload configuration via container
func systemdDaemonReload(ctx context.Context, conn context.Context, userMode bool) error {
	mode := "rootful"
	if userMode {
		mode = "rootless"
	}
	logger.Infof("Triggering systemd daemon-reload (%s)", mode)

	root := !userMode
	if err := runSystemctlCommand(conn, root, "daemon-reload", ""); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	logger.Infof("Completed systemd daemon-reload (%s)", mode)
	return nil
}

// systemdEnableService enables a service to start on boot
func systemdEnableService(ctx context.Context, conn context.Context, serviceName string, userMode bool) error {
	root := !userMode
	if err := runSystemctlCommand(conn, root, "enable", serviceName); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", serviceName, err)
	}
	logger.Infof("Enabled service: %s", serviceName)
	return nil
}

// systemdStartService starts a systemd service
func systemdStartService(ctx context.Context, conn context.Context, serviceName string, userMode bool) error {
	root := !userMode
	if err := runSystemctlCommand(conn, root, "start", serviceName); err != nil {
		return fmt.Errorf("failed to start service %s: %w", serviceName, err)
	}
	logger.Infof("Started service: %s", serviceName)
	return nil
}

// systemdRestartService restarts a systemd service
func systemdRestartService(ctx context.Context, conn context.Context, serviceName string, userMode bool) error {
	root := !userMode
	if err := runSystemctlCommand(conn, root, "restart", serviceName); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", serviceName, err)
	}
	logger.Infof("Restarted service: %s", serviceName)
	return nil
}

// systemdStopService stops a systemd service
func systemdStopService(ctx context.Context, conn context.Context, serviceName string, userMode bool) error {
	root := !userMode
	if err := runSystemctlCommand(conn, root, "stop", serviceName); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", serviceName, err)
	}
	logger.Infof("Stopped service: %s", serviceName)
	return nil
}

// deriveServiceName converts a Quadlet filename to systemd service name
func deriveServiceName(quadletFilename string) string {
	ext := filepath.Ext(quadletFilename)
	base := strings.TrimSuffix(filepath.Base(quadletFilename), ext)

	switch ext {
	case ".container":
		// myapp.container -> myapp.service
		return base + ".service"
	case ".volume":
		// data.volume -> data-volume.service
		return base + "-volume.service"
	case ".network":
		// app-net.network -> app-net-network.service
		return base + "-network.service"
	case ".kube":
		// webapp.kube -> webapp.service
		return base + ".service"
	case ".pod":
		// mypod.pod -> mypod-pod.service
		return base + "-pod.service"
	default:
		// Unknown type, assume base + .service
		return base + ".service"
	}
}

// determineChangeType analyzes object.Change to determine operation type
func determineChangeType(change *object.Change) string {
	if change == nil {
		return "unknown"
	}

	// From is empty and To is populated = create
	if change.From.Name == "" && change.To.Name != "" {
		return "create"
	}

	// From is populated and To is empty = delete
	if change.From.Name != "" && change.To.Name == "" {
		return "delete"
	}

	// From and To are both populated but different = rename
	if change.From.Name != "" && change.To.Name != "" && change.From.Name != change.To.Name {
		return "rename"
	}

	// From and To are the same = update
	if change.From.Name != "" && change.To.Name != "" && change.From.Name == change.To.Name {
		return "update"
	}

	return "unknown"
}

// Process handles periodic Git synchronization and change detection
func (q *Quadlet) Process(ctx, conn context.Context, skew int) {
	target := q.GetTarget()
	if target == nil {
		logger.Errorf("Quadlet target not initialized")
		return
	}

	// Sleep for skew milliseconds to distribute load
	time.Sleep(time.Duration(skew) * time.Millisecond)

	// Acquire target mutex lock
	target.mu.Lock()
	defer target.mu.Unlock()

	// Define Quadlet file extensions to monitor
	tags := []string{".container", ".volume", ".network", ".kube"}

	if q.initialRun {
		// First run: clone repository
		err := getRepo(target)
		if err != nil {
			logger.Errorf("Failed to clone repository %s: %v", target.url, err)
			return
		}

		// Initial deployment
		err = zeroToCurrent(ctx, conn, q, target, &tags)
		if err != nil {
			logger.Errorf("Error moving to current state: %v", err)
			return
		}
	}

	// Fetch updates and apply changes (runs on every iteration, including first)
	err := currentToLatest(ctx, conn, q, target, &tags)
	if err != nil {
		logger.Errorf("Error moving current to latest: %v", err)
		return
	}

	q.initialRun = false
}

// MethodEngine processes a single file change
func (q *Quadlet) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
	var changeType string
	var curr *string
	var prev *string

	// Determine change type and file names
	if change != nil {
		if change.From.Name != "" {
			prev = &change.From.Name
		}
		if change.To.Name != "" {
			curr = &change.To.Name
		}
		changeType = determineChangeType(change)
	}

	// Get Quadlet directory
	paths, err := GetQuadletDirectory(q.Root)
	if err != nil {
		return fmt.Errorf("failed to get Quadlet directory: %w", err)
	}

	// Ensure directory exists on HOST (must be done before fileTransferPodman)
	if err := q.ensureQuadletDirectory(conn); err != nil {
		return err
	}

	// Use FileTransfer method for copying files (same pattern as Systemd)
	// This creates a temporary container with bind mounts to access host filesystem
	ft := &FileTransfer{
		CommonMethod: CommonMethod{
			Name: q.Name,
		},
	}

	// Perform file operation based on change type
	switch changeType {
	case "create", "update":
		if curr == nil {
			return fmt.Errorf("change type %s but no current file name", changeType)
		}
		// Copy file from Git clone to Quadlet directory using fileTransferPodman
		if err := ft.fileTransferPodman(ctx, conn, path, paths.InputDirectory, nil); err != nil {
			return fmt.Errorf("failed to copy Quadlet file: %w", err)
		}
		logger.Infof("Placed Quadlet file: %s", filepath.Join(paths.InputDirectory, filepath.Base(*curr)))

	case "rename":
		// Remove old file, then copy new file
		if err := ft.fileTransferPodman(ctx, conn, path, paths.InputDirectory, prev); err != nil {
			return fmt.Errorf("failed to copy renamed Quadlet file: %w", err)
		}
		if curr != nil {
			logger.Infof("Renamed Quadlet file: %s", filepath.Join(paths.InputDirectory, filepath.Base(*curr)))
		}

	case "delete":
		// Remove file from Quadlet directory
		if prev != nil {
			if err := ft.fileTransferPodman(ctx, conn, deleteFile, paths.InputDirectory, prev); err != nil {
				return fmt.Errorf("failed to remove Quadlet file: %w", err)
			}
			logger.Infof("Removed Quadlet file: %s", filepath.Join(paths.InputDirectory, filepath.Base(*prev)))
		}
	}

	// Note: daemon-reload is batched in Apply(), not triggered here
	return nil
}

// Apply processes all file changes in a batch and triggers daemon-reload
func (q *Quadlet) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
	target := q.GetTarget()
	if target == nil {
		return fmt.Errorf("Quadlet target not initialized")
	}

	// Get filtered changes (use Glob pointer directly, applyChanges handles nil)
	changeMap, err := applyChanges(ctx, target, q.GetTargetPath(), q.Glob, currentState, desiredState, tags)
	if err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	// If no changes, nothing to do
	if len(changeMap) == 0 {
		logger.Infof("No Quadlet file changes detected for target %s", q.GetName())
		return nil
	}

	// Process each file change
	if err := runChanges(ctx, conn, q, changeMap); err != nil {
		return fmt.Errorf("failed to run changes: %w", err)
	}

	// Trigger daemon-reload (ONCE after all file changes)
	userMode := !q.Root
	if err := systemdDaemonReload(ctx, conn, userMode); err != nil {
		return fmt.Errorf("systemd daemon-reload failed: %w", err)
	}

	// If Enable is false, we're done
	if !q.Enable {
		logger.Infof("Quadlet target %s successfully processed (files placed, not enabled)", q.GetName())
		return nil
	}

	// Enable and start/restart services based on change type
	for change := range changeMap {
		if change.To.Name == "" {
			continue // Skip deletes for service start
		}

		serviceName := deriveServiceName(change.To.Name)
		changeType := determineChangeType(change)

		switch changeType {
		case "create":
			// Enable and start new services (enable with --now starts them)
			if err := systemdEnableService(ctx, conn, serviceName, userMode); err != nil {
				logger.Errorf("Failed to enable service %s: %v", serviceName, err)
				continue
			}

		case "update":
			// Restart on update if Restart=true
			if q.Restart {
				if err := systemdRestartService(ctx, conn, serviceName, userMode); err != nil {
					logger.Errorf("Failed to restart service %s: %v", serviceName, err)
				}
			}

		case "delete":
			// Stop and disable deleted services
			if change.From.Name != "" {
				deletedServiceName := deriveServiceName(change.From.Name)
				if err := systemdStopService(ctx, conn, deletedServiceName, userMode); err != nil {
					logger.Warnf("Failed to stop service %s: %v", deletedServiceName, err)
				}
			}
		}
	}

	logger.Infof("Quadlet target %s successfully processed", q.GetName())
	return nil
}
