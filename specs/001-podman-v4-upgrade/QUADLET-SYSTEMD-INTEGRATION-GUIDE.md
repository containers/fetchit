# Quadlet systemd Integration Guide for Go Applications

This guide provides comprehensive implementation guidance for integrating Podman Quadlet with systemd in a Go application, based on research and best practices.

## Table of Contents

1. [Overview](#overview)
2. [When to Trigger daemon-reload](#when-to-trigger-daemon-reload)
3. [How to Trigger daemon-reload](#how-to-trigger-daemon-reload)
4. [How Podman's systemd Generator Works](#how-podmans-systemd-generator-works)
5. [Error Detection](#error-detection)
6. [Best Practices for Service Lifecycle Management](#best-practices-for-service-lifecycle-management)
7. [Go Implementation Examples](#go-implementation-examples)
8. [Rootless vs Root Considerations](#rootless-vs-root-considerations)

## Overview

Podman Quadlet is a systemd generator that processes declarative container configuration files (`.container`, `.volume`, `.network`, `.pod`, `.kube`, `.build`, `.artifact`) and generates corresponding systemd service files. This integration allows containers to be managed natively through systemd.

## When to Trigger daemon-reload

### Rule 1: After Every Quadlet File Change

**Always trigger `daemon-reload` after modifying, adding, or removing Quadlet files.** The systemd generator only runs during:
- System boot
- When `systemctl daemon-reload` is executed

### Rule 2: Batch Operations are Recommended

When making multiple changes to Quadlet files:

1. **Make all file changes first** (create, update, delete multiple files)
2. **Run daemon-reload once** after all changes are complete
3. **Then start/restart services** as needed

**Why?** The `daemon-reload` operation is relatively expensive as it:
- Reruns all generators
- Reloads all unit files
- Recreates the entire dependency tree

However, it's designed to handle batch operations atomically - all sockets systemd listens on behalf of user configuration stay accessible during the reload.

### Rule 3: Detection of Need for Reload

Systemd will warn when a reload is needed. When checking service status, you'll see:

```
Warning: The unit file, source configuration file or drop-ins of [service].service changed on disk.
Run 'systemctl daemon-reload' to reload units.
```

### Recommended Workflow for Go Applications

```go
// Pseudocode for batch operations
func DeployQuadlets(quadletFiles []QuadletFile) error {
    // Step 1: Make all file changes
    for _, file := range quadletFiles {
        if err := writeQuadletFile(file); err != nil {
            return err
        }
    }

    // Step 2: Single daemon-reload after all changes
    if err := systemdDaemonReload(); err != nil {
        return err
    }

    // Step 3: Start/restart services as needed
    for _, file := range quadletFiles {
        serviceName := file.GetServiceName()
        if err := systemdStartService(serviceName); err != nil {
            return err
        }
    }

    return nil
}
```

## How to Trigger daemon-reload

### Option 1: Using go-systemd D-Bus Library (Recommended)

The official and most reliable method is using the D-Bus API via the `go-systemd` library.

**Installation:**

```bash
go get github.com/coreos/go-systemd/v22/dbus
```

**Implementation:**

```go
package systemd

import (
    "context"
    "fmt"

    "github.com/coreos/go-systemd/v22/dbus"
)

// DaemonReload instructs systemd to scan for and reload unit files.
// This is equivalent to running 'systemctl daemon-reload'.
func DaemonReload(ctx context.Context, userMode bool) error {
    var conn *dbus.Conn
    var err error

    if userMode {
        // Connect to user systemd instance
        conn, err = dbus.NewUserConnectionContext(ctx)
    } else {
        // Connect to system systemd instance
        conn, err = dbus.NewSystemdConnectionContext(ctx)
    }

    if err != nil {
        return fmt.Errorf("failed to connect to systemd: %w", err)
    }
    defer conn.Close()

    // Reload systemd configuration
    if err := conn.ReloadContext(ctx); err != nil {
        return fmt.Errorf("failed to reload systemd daemon: %w", err)
    }

    return nil
}

// DaemonReloadUser is a convenience wrapper for user mode
func DaemonReloadUser(ctx context.Context) error {
    return DaemonReload(ctx, true)
}

// DaemonReloadSystem is a convenience wrapper for system mode
func DaemonReloadSystem(ctx context.Context) error {
    return DaemonReload(ctx, false)
}
```

**Usage Example:**

```go
func main() {
    ctx := context.Background()

    // For rootless Podman (user services)
    if err := DaemonReloadUser(ctx); err != nil {
        log.Fatalf("Failed to reload user systemd: %v", err)
    }

    // For rootful Podman (system services)
    if err := DaemonReloadSystem(ctx); err != nil {
        log.Fatalf("Failed to reload system systemd: %v", err)
    }
}
```

### Option 2: Using systemctl Command (Fallback)

If D-Bus is unavailable, you can shell out to `systemctl`:

```go
package systemd

import (
    "context"
    "fmt"
    "os/exec"
)

// DaemonReloadCommand triggers daemon-reload using systemctl command
func DaemonReloadCommand(ctx context.Context, userMode bool) error {
    var cmd *exec.Cmd

    if userMode {
        cmd = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload")
    } else {
        cmd = exec.CommandContext(ctx, "systemctl", "daemon-reload")
    }

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("systemctl daemon-reload failed: %w, output: %s", err, output)
    }

    return nil
}
```

**Note:** The D-Bus method is preferred because it:
- Provides better error handling
- Doesn't require spawning shell processes
- Is more efficient
- Provides direct integration with systemd

## How Podman's systemd Generator Works

### Generator Execution

The Podman systemd generator (`/usr/libexec/podman/quadlet` or `/usr/lib/systemd/system-generators/podman-system-generator`) runs:

1. **During boot** - Automatically processed by systemd
2. **When daemon-reload is triggered** - Either manually or programmatically

### File Discovery Process

The generator scans specific search paths for Quadlet files:

**Rootful (System) Mode:**
```
/run/containers/systemd/          # Temporary quadlets (runtime)
/etc/containers/systemd/          # System administrator defined
/usr/share/containers/systemd/    # Distribution defined
```

**Rootless (User) Mode:**
```
$XDG_RUNTIME_DIR/containers/systemd/           # Typically /run/user/UID/containers/systemd/
$XDG_CONFIG_HOME/containers/systemd/           # Typically ~/.config/containers/systemd/
/etc/containers/systemd/users/$(UID)          # Per-user admin configs
/etc/containers/systemd/users/                 # Shared user configs
```

### Generation Process

1. **Scan**: Generator looks for files with recognized extensions
   - `.container` - Container definitions
   - `.volume` - Volume definitions
   - `.network` - Network definitions
   - `.pod` - Pod definitions
   - `.kube` - Kubernetes YAML
   - `.build` - Build instructions
   - `.artifact` - OCI artifacts

2. **Parse**: Each Quadlet file is parsed for syntax and semantics

3. **Generate**: For each valid Quadlet file, a corresponding `.service` file is generated
   - `my-app.container` → `my-app.service`
   - Generated files are not written to disk in the search paths
   - They exist in systemd's runtime generator directory

4. **Dependencies**: The generator resolves dependencies between Quadlet files
   - If a `.container` references a `.volume`, the volume service is created first
   - If a `.container` references a `.network`, the network service is created first

### Key Benefits

- **Automatic updates**: If a newer version of Podman is released with fixes or enhancements to the generator, services are updated automatically the next time `daemon-reload` runs
- **Declarative**: Focus on what you want, not how to achieve it
- **Native systemd integration**: Full access to systemd features like dependencies, restart policies, resource limits

## Error Detection

### Method 1: Using systemd-analyze (Recommended for Development)

The `systemd-analyze` command provides user-friendly error reporting:

```bash
# For user services
systemd-analyze --user --generators=true verify my-app.service

# For system services
systemd-analyze --generators=true verify my-app.service

# With custom unit directories
QUADLET_UNIT_DIRS=/path/to/quadlets systemd-analyze --user --generators=true verify my-app.service
```

**What it detects:**
- Syntax errors (e.g., `jImage` instead of `Image`)
- Invalid keys in Quadlet files
- Missing dependencies (e.g., referenced `.volume` file doesn't exist)
- Unit file configuration issues

### Method 2: Using podman-system-generator (Dry Run)

For more detailed generator output:

```bash
# For user services
/usr/lib/systemd/system-generators/podman-system-generator --user --dryrun

# For system services
/usr/lib/systemd/system-generators/podman-system-generator --dryrun

# Filter verbose output
/usr/lib/systemd/system-generators/podman-system-generator --user --dryrun 2>&1 | grep -i error
```

**What it provides:**
- Raw generator output
- Detailed error messages
- Shows what service files would be generated

### Method 3: Programmatic Error Detection in Go

```go
package systemd

import (
    "context"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// ValidateQuadletFile validates a Quadlet file before deployment
func ValidateQuadletFile(ctx context.Context, quadletPath string, userMode bool) error {
    // Get the service name from the quadlet file
    serviceName := strings.TrimSuffix(filepath.Base(quadletPath), filepath.Ext(quadletPath)) + ".service"

    args := []string{"--generators=true", "verify", serviceName}
    if userMode {
        args = append([]string{"--user"}, args...)
    }

    // Set QUADLET_UNIT_DIRS to include our quadlet directory
    env := os.Environ()
    quadletDir := filepath.Dir(quadletPath)
    env = append(env, fmt.Sprintf("QUADLET_UNIT_DIRS=%s", quadletDir))

    cmd := exec.CommandContext(ctx, "systemd-analyze", args...)
    cmd.Env = env

    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("quadlet validation failed: %w\nOutput: %s", err, output)
    }

    return nil
}

// ValidateQuadletFileWithGenerator uses the Podman generator for validation
func ValidateQuadletFileWithGenerator(ctx context.Context, userMode bool) (string, error) {
    generatorPath := "/usr/lib/systemd/system-generators/podman-system-generator"

    args := []string{"--dryrun"}
    if userMode {
        args = append([]string{"--user"}, args...)
    }

    cmd := exec.CommandContext(ctx, generatorPath, args...)
    output, err := cmd.CombinedOutput()

    if err != nil {
        return string(output), fmt.Errorf("generator dry run failed: %w", err)
    }

    return string(output), nil
}
```

### Method 4: Checking Service Status After daemon-reload

```go
package systemd

import (
    "context"
    "fmt"

    "github.com/coreos/go-systemd/v22/dbus"
)

// CheckServiceExists verifies that a service was generated successfully
func CheckServiceExists(ctx context.Context, serviceName string, userMode bool) error {
    var conn *dbus.Conn
    var err error

    if userMode {
        conn, err = dbus.NewUserConnectionContext(ctx)
    } else {
        conn, err = dbus.NewSystemdConnectionContext(ctx)
    }

    if err != nil {
        return fmt.Errorf("failed to connect to systemd: %w", err)
    }
    defer conn.Close()

    // Try to get the unit properties
    units, err := conn.ListUnitsByNamesContext(ctx, []string{serviceName})
    if err != nil {
        return fmt.Errorf("failed to list units: %w", err)
    }

    if len(units) == 0 {
        return fmt.Errorf("service %s was not generated (Quadlet file may have errors)", serviceName)
    }

    unit := units[0]
    if unit.LoadState == "not-found" {
        return fmt.Errorf("service %s not found (Quadlet generation likely failed)", serviceName)
    }

    if unit.LoadState == "error" {
        return fmt.Errorf("service %s has load errors", serviceName)
    }

    return nil
}
```

### Common Error Scenarios

| Error | Cause | Detection Method |
|-------|-------|------------------|
| Unsupported key | Using incorrect key name (e.g., `jImage` instead of `Image`) | systemd-analyze, generator dry-run |
| Missing dependency | Referencing non-existent `.volume` or `.network` file | systemd-analyze, CheckServiceExists |
| Syntax error | Invalid INI format or Quadlet syntax | systemd-analyze |
| Permission denied | Incorrect file permissions or ownership | Service status check, journalctl |
| Image pull timeout | TimeoutStartSec too short for image pull | journalctl, service status |

### Monitoring Service Generation

```go
package systemd

import (
    "context"
    "fmt"
    "strings"

    "github.com/coreos/go-systemd/v22/dbus"
)

// DeployQuadletWithValidation deploys a Quadlet file with full validation
func DeployQuadletWithValidation(ctx context.Context, quadletPath string, userMode bool) error {
    // Step 1: Validate the Quadlet file syntax
    if err := ValidateQuadletFile(ctx, quadletPath, userMode); err != nil {
        return fmt.Errorf("quadlet validation failed: %w", err)
    }

    // Step 2: Trigger daemon-reload
    if err := DaemonReload(ctx, userMode); err != nil {
        return fmt.Errorf("daemon-reload failed: %w", err)
    }

    // Step 3: Verify the service was generated
    serviceName := getServiceNameFromQuadlet(quadletPath)
    if err := CheckServiceExists(ctx, serviceName, userMode); err != nil {
        return fmt.Errorf("service generation verification failed: %w", err)
    }

    return nil
}

func getServiceNameFromQuadlet(quadletPath string) string {
    base := filepath.Base(quadletPath)
    ext := filepath.Ext(base)
    name := strings.TrimSuffix(base, ext)
    return name + ".service"
}
```

## Best Practices for Service Lifecycle Management

### 1. File Placement and Atomic Operations

**Use atomic file operations** to prevent race conditions:

```go
package systemd

import (
    "fmt"
    "io"
    "os"
    "path/filepath"
)

// WriteQuadletFileAtomic writes a Quadlet file atomically
func WriteQuadletFileAtomic(path string, content []byte, perm os.FileMode) error {
    // Create temp file in the same directory for atomic rename
    dir := filepath.Dir(path)
    tmpFile, err := os.CreateTemp(dir, ".quadlet-tmp-*")
    if err != nil {
        return fmt.Errorf("failed to create temp file: %w", err)
    }
    tmpPath := tmpFile.Name()

    // Clean up temp file on error
    defer func() {
        if tmpFile != nil {
            tmpFile.Close()
            os.Remove(tmpPath)
        }
    }()

    // Write content
    if _, err := tmpFile.Write(content); err != nil {
        return fmt.Errorf("failed to write temp file: %w", err)
    }

    // Sync to disk
    if err := tmpFile.Sync(); err != nil {
        return fmt.Errorf("failed to sync temp file: %w", err)
    }

    // Close before rename
    if err := tmpFile.Close(); err != nil {
        return fmt.Errorf("failed to close temp file: %w", err)
    }
    tmpFile = nil // Prevent deferred cleanup

    // Set permissions
    if err := os.Chmod(tmpPath, perm); err != nil {
        os.Remove(tmpPath)
        return fmt.Errorf("failed to set permissions: %w", err)
    }

    // Atomic rename
    if err := os.Rename(tmpPath, path); err != nil {
        os.Remove(tmpPath)
        return fmt.Errorf("failed to rename temp file: %w", err)
    }

    return nil
}
```

### 2. Restart Policies in Quadlet Files

**Always specify restart policies** in your `.container` files:

```ini
[Container]
Image=myapp:latest
# ... other settings ...

[Service]
Restart=always
TimeoutStartSec=300
```

**Restart Policy Options:**
- `Restart=no` - Never restart (default)
- `Restart=on-failure` - Restart only on failure
- `Restart=always` - Always restart (recommended for long-running services)
- `Restart=on-abnormal` - Restart on abnormal termination

### 3. Handling Service Dependencies

```ini
# database.container
[Container]
Image=postgres:15
ContainerName=myapp-db

# app.container
[Container]
Image=myapp:latest
ContainerName=myapp

[Service]
# Wait for database service to start
After=database.service
Requires=database.service
```

### 4. Timeout Configuration

Podman may need to pull images, which can exceed systemd's default 90-second timeout:

```ini
[Service]
# Increase timeout for image pulls (5 minutes)
TimeoutStartSec=300
# Increase stop timeout if graceful shutdown takes time
TimeoutStopSec=60
```

### 5. Complete Service Management Functions

```go
package systemd

import (
    "context"
    "fmt"
    "time"

    "github.com/coreos/go-systemd/v22/dbus"
)

// StartService starts a systemd service
func StartService(ctx context.Context, serviceName string, userMode bool) error {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return err
    }
    defer conn.Close()

    responseChan := make(chan string)
    _, err = conn.StartUnitContext(ctx, serviceName, "replace", responseChan)
    if err != nil {
        return fmt.Errorf("failed to start service %s: %w", serviceName, err)
    }

    // Wait for job completion
    job := <-responseChan
    if job != "done" {
        return fmt.Errorf("start job for %s completed with status: %s", serviceName, job)
    }

    return nil
}

// StopService stops a systemd service
func StopService(ctx context.Context, serviceName string, userMode bool) error {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return err
    }
    defer conn.Close()

    responseChan := make(chan string)
    _, err = conn.StopUnitContext(ctx, serviceName, "replace", responseChan)
    if err != nil {
        return fmt.Errorf("failed to stop service %s: %w", serviceName, err)
    }

    // Wait for job completion
    job := <-responseChan
    if job != "done" {
        return fmt.Errorf("stop job for %s completed with status: %s", serviceName, job)
    }

    return nil
}

// RestartService restarts a systemd service
func RestartService(ctx context.Context, serviceName string, userMode bool) error {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return err
    }
    defer conn.Close()

    responseChan := make(chan string)
    _, err = conn.RestartUnitContext(ctx, serviceName, "replace", responseChan)
    if err != nil {
        return fmt.Errorf("failed to restart service %s: %w", serviceName, err)
    }

    // Wait for job completion
    job := <-responseChan
    if job != "done" {
        return fmt.Errorf("restart job for %s completed with status: %s", serviceName, job)
    }

    return nil
}

// EnableService enables a systemd service to start on boot
func EnableService(ctx context.Context, serviceName string, userMode bool) error {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return err
    }
    defer conn.Close()

    _, _, err = conn.EnableUnitFilesContext(ctx, []string{serviceName}, false, true)
    if err != nil {
        return fmt.Errorf("failed to enable service %s: %w", serviceName, err)
    }

    // Reload after enabling
    return conn.ReloadContext(ctx)
}

// DisableService disables a systemd service
func DisableService(ctx context.Context, serviceName string, userMode bool) error {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return err
    }
    defer conn.Close()

    _, err = conn.DisableUnitFilesContext(ctx, []string{serviceName}, false)
    if err != nil {
        return fmt.Errorf("failed to disable service %s: %w", serviceName, err)
    }

    // Reload after disabling
    return conn.ReloadContext(ctx)
}

// GetServiceStatus retrieves the current status of a service
func GetServiceStatus(ctx context.Context, serviceName string, userMode bool) (*ServiceStatus, error) {
    conn, err := getConnection(ctx, userMode)
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    units, err := conn.ListUnitsByNamesContext(ctx, []string{serviceName})
    if err != nil {
        return nil, fmt.Errorf("failed to get service status: %w", err)
    }

    if len(units) == 0 {
        return nil, fmt.Errorf("service %s not found", serviceName)
    }

    unit := units[0]
    return &ServiceStatus{
        Name:        unit.Name,
        LoadState:   unit.LoadState,
        ActiveState: unit.ActiveState,
        SubState:    unit.SubState,
        Description: unit.Description,
    }, nil
}

type ServiceStatus struct {
    Name        string
    LoadState   string // "loaded", "not-found", "error", etc.
    ActiveState string // "active", "inactive", "failed", etc.
    SubState    string // "running", "dead", "exited", etc.
    Description string
}

// Helper function to get D-Bus connection
func getConnection(ctx context.Context, userMode bool) (*dbus.Conn, error) {
    if userMode {
        return dbus.NewUserConnectionContext(ctx)
    }
    return dbus.NewSystemdConnectionContext(ctx)
}
```

### 6. Cleanup and Removal

```go
package systemd

import (
    "context"
    "fmt"
    "os"
)

// RemoveQuadletService properly removes a Quadlet service
func RemoveQuadletService(ctx context.Context, quadletPath string, userMode bool) error {
    serviceName := getServiceNameFromQuadlet(quadletPath)

    // Step 1: Stop the service if running
    if err := StopService(ctx, serviceName, userMode); err != nil {
        // Log but don't fail if service is already stopped
        fmt.Printf("Warning: failed to stop service %s: %v\n", serviceName, err)
    }

    // Step 2: Disable the service
    if err := DisableService(ctx, serviceName, userMode); err != nil {
        // Log but don't fail if service is already disabled
        fmt.Printf("Warning: failed to disable service %s: %v\n", serviceName, err)
    }

    // Step 3: Remove the Quadlet file
    if err := os.Remove(quadletPath); err != nil && !os.IsNotExist(err) {
        return fmt.Errorf("failed to remove quadlet file: %w", err)
    }

    // Step 4: Reload daemon to remove the generated service
    if err := DaemonReload(ctx, userMode); err != nil {
        return fmt.Errorf("failed to reload daemon after removal: %w", err)
    }

    return nil
}
```

### 7. Auto-Update Configuration

Enable automatic container updates in Quadlet files:

```ini
[Container]
Image=myapp:latest
AutoUpdate=registry

[Service]
Restart=always

[Install]
WantedBy=default.target
```

Then enable the Podman auto-update timer:

```go
package systemd

// EnablePodmanAutoUpdate enables automatic container updates
func EnablePodmanAutoUpdate(ctx context.Context, userMode bool) error {
    // Enable and start the auto-update timer
    if err := EnableService(ctx, "podman-auto-update.timer", userMode); err != nil {
        return fmt.Errorf("failed to enable podman-auto-update.timer: %w", err)
    }

    if err := StartService(ctx, "podman-auto-update.timer", userMode); err != nil {
        return fmt.Errorf("failed to start podman-auto-update.timer: %w", err)
    }

    return nil
}
```

## Go Implementation Examples

### Complete Example: Deploying a Quadlet Application

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "path/filepath"
    "time"
)

func main() {
    ctx := context.Background()

    // Configuration
    userMode := true // Set to false for rootful
    appName := "myapp"

    // Define Quadlet content
    quadletContent := `[Container]
Image=docker.io/library/nginx:latest
ContainerName=myapp-nginx
PublishPort=8080:80

[Service]
Restart=always
TimeoutStartSec=300

[Install]
WantedBy=default.target
`

    // Get Quadlet directory
    quadletDir, err := getQuadletDirectory(userMode)
    if err != nil {
        log.Fatalf("Failed to get quadlet directory: %v", err)
    }

    // Ensure directory exists
    if err := os.MkdirAll(quadletDir, 0755); err != nil {
        log.Fatalf("Failed to create quadlet directory: %v", err)
    }

    quadletPath := filepath.Join(quadletDir, appName+".container")

    // Deploy the application
    if err := deployQuadletApplication(ctx, quadletPath, quadletContent, userMode); err != nil {
        log.Fatalf("Deployment failed: %v", err)
    }

    fmt.Printf("Successfully deployed %s\n", appName)

    // Monitor the service
    time.Sleep(2 * time.Second)
    status, err := GetServiceStatus(ctx, appName+".service", userMode)
    if err != nil {
        log.Fatalf("Failed to get service status: %v", err)
    }

    fmt.Printf("Service Status:\n")
    fmt.Printf("  Name: %s\n", status.Name)
    fmt.Printf("  Load State: %s\n", status.LoadState)
    fmt.Printf("  Active State: %s\n", status.ActiveState)
    fmt.Printf("  Sub State: %s\n", status.SubState)
}

func deployQuadletApplication(ctx context.Context, quadletPath, content string, userMode bool) error {
    // Step 1: Write Quadlet file atomically
    fmt.Printf("Writing Quadlet file to %s\n", quadletPath)
    if err := WriteQuadletFileAtomic(quadletPath, []byte(content), 0644); err != nil {
        return fmt.Errorf("failed to write quadlet file: %w", err)
    }

    // Step 2: Validate the Quadlet file
    fmt.Println("Validating Quadlet file...")
    if err := ValidateQuadletFile(ctx, quadletPath, userMode); err != nil {
        return fmt.Errorf("quadlet validation failed: %w", err)
    }

    // Step 3: Trigger daemon-reload
    fmt.Println("Reloading systemd daemon...")
    if err := DaemonReload(ctx, userMode); err != nil {
        return fmt.Errorf("daemon-reload failed: %w", err)
    }

    // Step 4: Verify service was generated
    serviceName := getServiceNameFromQuadlet(quadletPath)
    fmt.Printf("Verifying service %s was generated...\n", serviceName)
    if err := CheckServiceExists(ctx, serviceName, userMode); err != nil {
        return fmt.Errorf("service verification failed: %w", err)
    }

    // Step 5: Enable the service
    fmt.Printf("Enabling service %s...\n", serviceName)
    if err := EnableService(ctx, serviceName, userMode); err != nil {
        return fmt.Errorf("failed to enable service: %w", err)
    }

    // Step 6: Start the service
    fmt.Printf("Starting service %s...\n", serviceName)
    if err := StartService(ctx, serviceName, userMode); err != nil {
        return fmt.Errorf("failed to start service: %w", err)
    }

    return nil
}

func getQuadletDirectory(userMode bool) (string, error) {
    if userMode {
        home := os.Getenv("HOME")
        if home == "" {
            return "", fmt.Errorf("HOME environment variable not set")
        }

        // Use XDG_CONFIG_HOME if set, otherwise use default
        configHome := os.Getenv("XDG_CONFIG_HOME")
        if configHome == "" {
            configHome = filepath.Join(home, ".config")
        }

        return filepath.Join(configHome, "containers", "systemd"), nil
    }

    // System-wide Quadlets
    return "/etc/containers/systemd", nil
}
```

### Example: Batch Deployment with Rollback

```go
package main

import (
    "context"
    "fmt"
)

type QuadletDeployment struct {
    Name    string
    Path    string
    Content string
}

func deployMultipleQuadlets(ctx context.Context, deployments []QuadletDeployment, userMode bool) error {
    var deployedFiles []string

    // Step 1: Write all Quadlet files
    for _, deploy := range deployments {
        fmt.Printf("Writing %s...\n", deploy.Name)
        if err := WriteQuadletFileAtomic(deploy.Path, []byte(deploy.Content), 0644); err != nil {
            // Rollback on error
            rollbackDeployments(ctx, deployedFiles, userMode)
            return fmt.Errorf("failed to write %s: %w", deploy.Name, err)
        }
        deployedFiles = append(deployedFiles, deploy.Path)
    }

    // Step 2: Validate all files
    for _, deploy := range deployments {
        fmt.Printf("Validating %s...\n", deploy.Name)
        if err := ValidateQuadletFile(ctx, deploy.Path, userMode); err != nil {
            // Rollback on validation error
            rollbackDeployments(ctx, deployedFiles, userMode)
            return fmt.Errorf("validation failed for %s: %w", deploy.Name, err)
        }
    }

    // Step 3: Single daemon-reload for all changes
    fmt.Println("Reloading systemd daemon...")
    if err := DaemonReload(ctx, userMode); err != nil {
        rollbackDeployments(ctx, deployedFiles, userMode)
        return fmt.Errorf("daemon-reload failed: %w", err)
    }

    // Step 4: Verify all services were generated
    for _, deploy := range deployments {
        serviceName := getServiceNameFromQuadlet(deploy.Path)
        fmt.Printf("Verifying %s...\n", serviceName)
        if err := CheckServiceExists(ctx, serviceName, userMode); err != nil {
            rollbackDeployments(ctx, deployedFiles, userMode)
            return fmt.Errorf("service verification failed for %s: %w", serviceName, err)
        }
    }

    // Step 5: Start all services
    for _, deploy := range deployments {
        serviceName := getServiceNameFromQuadlet(deploy.Path)
        fmt.Printf("Starting %s...\n", serviceName)
        if err := StartService(ctx, serviceName, userMode); err != nil {
            return fmt.Errorf("failed to start %s: %w", serviceName, err)
        }
    }

    return nil
}

func rollbackDeployments(ctx context.Context, files []string, userMode bool) {
    fmt.Println("Rolling back deployments...")
    for _, file := range files {
        os.Remove(file)
    }
    // Trigger daemon-reload to clean up
    DaemonReload(ctx, userMode)
}
```

## Rootless vs Root Considerations

### Environment Variables

**Critical for rootless mode:**

```go
package systemd

import (
    "fmt"
    "os"
    "strconv"
)

// SetupRootlessEnvironment ensures proper environment for rootless Podman
func SetupRootlessEnvironment() error {
    // XDG_RUNTIME_DIR is critical for rootless systemd
    xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
    if xdgRuntimeDir == "" {
        uid := os.Getuid()
        xdgRuntimeDir = fmt.Sprintf("/run/user/%d", uid)

        // Set the environment variable
        if err := os.Setenv("XDG_RUNTIME_DIR", xdgRuntimeDir); err != nil {
            return fmt.Errorf("failed to set XDG_RUNTIME_DIR: %w", err)
        }
    }

    return nil
}
```

### File Paths and Permissions

| Aspect | Rootful (System) | Rootless (User) |
|--------|------------------|-----------------|
| Quadlet directory | `/etc/containers/systemd/` | `~/.config/containers/systemd/` |
| File ownership | root:root | user:user |
| File permissions | 0644 | 0644 |
| systemctl command | `systemctl` | `systemctl --user` |
| D-Bus connection | System bus | User session bus |
| Required privileges | root or sudo | Regular user |

### Connection Differences

```go
package systemd

import (
    "context"
    "fmt"

    "github.com/coreos/go-systemd/v22/dbus"
)

// GetSystemdConnection returns appropriate connection based on mode
func GetSystemdConnection(ctx context.Context, userMode bool) (*dbus.Conn, error) {
    if userMode {
        // Ensure XDG_RUNTIME_DIR is set for user mode
        if err := SetupRootlessEnvironment(); err != nil {
            return nil, err
        }
        return dbus.NewUserConnectionContext(ctx)
    }

    // System connection requires root privileges
    return dbus.NewSystemdConnectionContext(ctx)
}
```

### Systemd User Instance

**Important:** For rootless mode, the systemd user instance must be running:

```bash
# Enable lingering for user (allows services to run when user is not logged in)
sudo loginctl enable-linger $USER
```

```go
package systemd

import (
    "context"
    "os/exec"
    "os/user"
)

// EnableUserLingering enables systemd user instance to persist after logout
func EnableUserLingering(ctx context.Context, username string) error {
    cmd := exec.CommandContext(ctx, "loginctl", "enable-linger", username)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("failed to enable user lingering: %w", err)
    }
    return nil
}

// EnableCurrentUserLingering enables lingering for the current user
func EnableCurrentUserLingering(ctx context.Context) error {
    currentUser, err := user.Current()
    if err != nil {
        return fmt.Errorf("failed to get current user: %w", err)
    }
    return EnableUserLingering(ctx, currentUser.Username)
}
```

## Summary and Recommendations

### Key Takeaways

1. **Always trigger daemon-reload** after Quadlet file changes
2. **Batch operations** - make all file changes, then single daemon-reload
3. **Use D-Bus API** via go-systemd for programmatic integration
4. **Validate before deploying** using systemd-analyze or generator dry-run
5. **Handle errors gracefully** with proper rollback mechanisms
6. **Set appropriate timeouts** for image pulls (300+ seconds)
7. **Use atomic file operations** to prevent race conditions
8. **Configure restart policies** in [Service] section
9. **For rootless mode**, ensure XDG_RUNTIME_DIR is set
10. **Enable user lingering** for persistent rootless services

### Recommended Workflow

```
1. Validate Quadlet syntax (systemd-analyze)
2. Write Quadlet file(s) atomically
3. Trigger daemon-reload (once for batch)
4. Verify service generation (CheckServiceExists)
5. Enable service (if needed)
6. Start service
7. Monitor status (GetServiceStatus, journalctl)
```

### Error Handling Strategy

```
- Validate early (before daemon-reload)
- Use atomic operations (prevent partial writes)
- Verify after reload (check service exists)
- Implement rollback (on deployment failure)
- Log errors (journalctl integration)
- Provide clear error messages
```

## References and Sources

### Documentation
- [Podman systemd.unit Documentation](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Make systemd better for Podman with Quadlet - Red Hat Blog](https://www.redhat.com/en/blog/quadlet-podman)
- [Quadlet: Running Podman containers under systemd](https://mo8it.com/blog/quadlet/)
- [Podman Quadlets with Podman Desktop](https://podman-desktop.io/blog/podman-quadlet)

### Programmatic Integration
- [Programmatically creating a Quadlet - GitHub Discussion](https://github.com/containers/podman/discussions/21435)
- [go-systemd v22 dbus package](https://pkg.go.dev/github.com/coreos/go-systemd/v22/dbus)
- [GitHub: coreos/go-systemd](https://github.com/coreos/go-systemd)

### Error Detection and Debugging
- [Quadlets debugging with systemd-analyze - GitHub Discussion](https://github.com/containers/podman/discussions/24891)
- [systemctl daemon-reload - Detecting when reload is needed](https://www.baeldung.com/linux/systemctl-daemon-reload)
- [What does systemctl daemon-reload do? - Linux Audit](https://linux-audit.com/systemd/faq/what-does-systemctl-daemon-reload-do/)

### Best Practices
- [systemctl Daemon Reload - LabEx Tutorial](https://labex.io/tutorials/linux-linux-systemctl-daemon-reload-390500)
- [Manage Systemd Services with systemctl - DigitalOcean](https://www.digitalocean.com/community/tutorials/how-to-use-systemctl-to-manage-systemd-services-and-units)
- [systemd/User - ArchWiki](https://wiki.archlinux.org/title/Systemd/User)
- [Automatic container updates with Podman quadlets](https://major.io/p/podman-quadlet-automatic-updates/)

### API References
- [systemctl man page](https://www.freedesktop.org/software/systemd/man/latest/systemctl.html)
- [go-systemd methods.go](https://github.com/coreos/go-systemd/blob/main/dbus/methods.go)
