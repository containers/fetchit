// Package engine provides deployment methods for fetchit
package engine

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coreos/go-systemd/v22/dbus"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

// ensureQuadletDirectory creates the Quadlet directory with proper permissions if it doesn't exist
func (q *Quadlet) ensureQuadletDirectory() error {
	paths, err := GetQuadletDirectory(q.Root)
	if err != nil {
		return fmt.Errorf("failed to get Quadlet directory: %w", err)
	}

	// Create directory with 0755 permissions (drwxr-xr-x)
	if err := os.MkdirAll(paths.InputDirectory, 0755); err != nil {
		return fmt.Errorf("failed to create Quadlet directory %s: %w", paths.InputDirectory, err)
	}

	return nil
}

// systemdDaemonReload triggers systemd to reload configuration via D-Bus
func systemdDaemonReload(ctx context.Context, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	if err := conn.ReloadContext(ctx); err != nil {
		return fmt.Errorf("failed to reload systemd daemon: %w", err)
	}

	mode := "rootful"
	if userMode {
		mode = "rootless"
	}
	logger.Infof("Triggered systemd daemon-reload (%s)", mode)

	return nil
}

// verifyServiceExists checks if systemd generated the service from Quadlet file
func verifyServiceExists(ctx context.Context, serviceName string, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	// Get unit properties to verify it exists
	_, err = conn.GetUnitPropertiesContext(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("service %s not found (Quadlet file may have syntax errors): %w", serviceName, err)
	}

	return nil
}

// systemdEnableService enables a service to start on boot
func systemdEnableService(ctx context.Context, serviceName string, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	// Enable the service
	_, _, err = conn.EnableUnitFilesContext(ctx, []string{serviceName}, false, true)
	if err != nil {
		return fmt.Errorf("failed to enable service %s: %w", serviceName, err)
	}

	logger.Infof("Enabled service: %s", serviceName)
	return nil
}

// systemdStartService starts a systemd service
func systemdStartService(ctx context.Context, serviceName string, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	// Start the service
	_, err = conn.StartUnitContext(ctx, serviceName, "replace", nil)
	if err != nil {
		return fmt.Errorf("failed to start service %s: %w", serviceName, err)
	}

	logger.Infof("Started service: %s", serviceName)
	return nil
}

// systemdRestartService restarts a systemd service
func systemdRestartService(ctx context.Context, serviceName string, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	// Restart the service
	_, err = conn.RestartUnitContext(ctx, serviceName, "replace", nil)
	if err != nil {
		return fmt.Errorf("failed to restart service %s: %w", serviceName, err)
	}

	logger.Infof("Restarted service: %s", serviceName)
	return nil
}

// systemdStopService stops a systemd service
func systemdStopService(ctx context.Context, serviceName string, userMode bool) error {
	var conn *dbus.Conn
	var err error

	if userMode {
		conn, err = dbus.NewUserConnectionContext(ctx)
	} else {
		conn, err = dbus.NewSystemConnectionContext(ctx)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to systemd D-Bus: %w", err)
	}
	defer conn.Close()

	// Stop the service
	_, err = conn.StopUnitContext(ctx, serviceName, "replace", nil)
	if err != nil {
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

// copyFile copies a file with appropriate permissions
func copyFile(src, dst string) error {
	// Open source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", src, err)
	}
	defer sourceFile.Close()

	// Create destination file with 0644 permissions (-rw-r--r--)
	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer destFile.Close()

	// Copy content
	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return fmt.Errorf("failed to copy file content: %w", err)
	}

	return nil
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

	// Ensure directory exists
	if err := q.ensureQuadletDirectory(); err != nil {
		return err
	}

	// Perform file operation based on change type
	switch changeType {
	case "create", "update":
		if curr == nil {
			return fmt.Errorf("change type %s but no current file name", changeType)
		}
		// Copy file from Git clone to Quadlet directory
		src := filepath.Join(path, *curr)
		dst := filepath.Join(paths.InputDirectory, filepath.Base(*curr))
		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("failed to copy Quadlet file: %w", err)
		}
		logger.Infof("Placed Quadlet file: %s", dst)

	case "rename":
		// Remove old file
		if prev != nil {
			oldDst := filepath.Join(paths.InputDirectory, filepath.Base(*prev))
			if err := os.Remove(oldDst); err != nil && !os.IsNotExist(err) {
				logger.Warnf("Failed to remove old Quadlet file %s: %v", oldDst, err)
			}
		}
		// Copy new file
		if curr != nil {
			src := filepath.Join(path, *curr)
			dst := filepath.Join(paths.InputDirectory, filepath.Base(*curr))
			if err := copyFile(src, dst); err != nil {
				return fmt.Errorf("failed to copy renamed Quadlet file: %w", err)
			}
			logger.Infof("Renamed Quadlet file: %s", dst)
		}

	case "delete":
		// Remove file from Quadlet directory
		if prev != nil {
			dst := filepath.Join(paths.InputDirectory, filepath.Base(*prev))
			if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove Quadlet file: %w", err)
			}
			logger.Infof("Removed Quadlet file: %s", dst)
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
	if err := systemdDaemonReload(ctx, userMode); err != nil {
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

		// Verify service was generated
		if err := verifyServiceExists(ctx, serviceName, userMode); err != nil {
			logger.Warnf("Service %s not generated (Quadlet file may have errors): %v", serviceName, err)
			continue
		}

		switch changeType {
		case "create":
			// Enable and start new services
			if err := systemdEnableService(ctx, serviceName, userMode); err != nil {
				logger.Errorf("Failed to enable service %s: %v", serviceName, err)
				continue
			}
			if err := systemdStartService(ctx, serviceName, userMode); err != nil {
				logger.Errorf("Failed to start service %s: %v", serviceName, err)
			}

		case "update":
			// Restart on update if Restart=true
			if q.Restart {
				if err := systemdRestartService(ctx, serviceName, userMode); err != nil {
					logger.Errorf("Failed to restart service %s: %v", serviceName, err)
				}
			}

		case "delete":
			// Stop and disable deleted services
			if change.From.Name != "" {
				deletedServiceName := deriveServiceName(change.From.Name)
				if err := systemdStopService(ctx, deletedServiceName, userMode); err != nil {
					logger.Warnf("Failed to stop service %s: %v", deletedServiceName, err)
				}
			}
		}
	}

	logger.Infof("Quadlet target %s successfully processed", q.GetName())
	return nil
}
