# Data Model: Quadlet Container Deployment

**Feature**: Quadlet Container Deployment
**Date**: 2025-12-30
**Status**: Phase 1 - Design

## Overview

This document defines the data structures and models for implementing Quadlet support in fetchit. The design follows the existing Method interface pattern used by systemd, kube, filetransfer, and other deployment methods.

---

## 1. Quadlet Method Structure

### Quadlet Struct Definition

```go
// Quadlet implements the Method interface for Podman Quadlet deployments
// Quadlet is a systemd generator that processes declarative container
// configuration files (.container, .volume, .network, .kube) and generates
// corresponding systemd service files.
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
    // Inherited from CommonMethod, used to determine whether to perform
    // initial clone or just fetch updates
    // (Not serialized, managed by engine)
}
```

### CommonMethod Fields (Inherited)

```go
// CommonMethod is embedded in Quadlet and provides:
type CommonMethod struct {
    // Name is the unique identifier for this target
    Name string `mapstructure:"name"`

    // URL is the Git repository URL containing Quadlet files
    URL string `mapstructure:"url"`

    // Branch is the Git branch to monitor
    Branch string `mapstructure:"branch"`

    // TargetPath is the directory within the repo containing Quadlet files
    TargetPath string `mapstructure:"targetPath"`

    // Glob is the pattern for filtering files (default: "**")
    // For Quadlet, useful patterns:
    // - "**/*.container" - Only container files
    // - "**/*.{container,volume,network}" - Multiple types
    Glob string `mapstructure:"glob"`

    // Schedule is the cron expression for polling interval
    Schedule string `mapstructure:"schedule"`

    // initialRun tracks first execution
    initialRun bool
}
```

---

## 2. File Metadata Structures

### QuadletFileMetadata

```go
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
    ChangeType ChangeType
}

// QuadletFileType represents the type of Quadlet file
type QuadletFileType string

const (
    QuadletContainer QuadletFileType = "container"
    QuadletVolume    QuadletFileType = "volume"
    QuadletNetwork   QuadletFileType = "network"
    QuadletKube      QuadletFileType = "kube"
)

// ChangeType represents the type of file operation
type ChangeType string

const (
    ChangeCreate ChangeType = "create"
    ChangeUpdate ChangeType = "update"
    ChangeRename ChangeType = "rename"
    ChangeDelete ChangeType = "delete"
)
```

---

## 3. Directory Mapping Configuration

### Directory Path Resolution

```go
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
        return QuadletDirectoryPaths{}, fmt.Errorf("HOME environment variable not set")
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
```

---

## 4. State Management

### Deployment State Tracking

```go
// QuadletDeploymentState tracks the state of a Quadlet deployment
// This is not persisted but maintained in memory during execution
type QuadletDeploymentState struct {
    // TargetName is the name of the target being deployed
    TargetName string

    // LastDeployedHash is the Git commit hash of the last successful deployment
    LastDeployedHash plumbing.Hash

    // DeployedServices is a map of service names to their deployment status
    DeployedServices map[string]ServiceDeploymentStatus

    // LastDaemonReload is the timestamp of the last daemon-reload operation
    LastDaemonReload time.Time
}

// ServiceDeploymentStatus represents the deployment status of a systemd service
type ServiceDeploymentStatus struct {
    // ServiceName is the systemd service name (e.g., "myapp.service")
    ServiceName string

    // QuadletFile is the source Quadlet file (e.g., "myapp.container")
    QuadletFile string

    // Enabled indicates if the service is enabled to start on boot
    Enabled bool

    // Active indicates if the service is currently running
    Active bool

    // LoadState is the systemd load state ("loaded", "not-found", "error")
    LoadState string

    // ActiveState is the systemd active state ("active", "inactive", "failed")
    ActiveState string

    // SubState is the systemd sub-state ("running", "dead", "exited")
    SubState string
}
```

---

## 5. Configuration Schema (YAML)

### Fetchit Configuration Example

```yaml
# Quadlet deployment target configuration
targets:
  - name: webapp-quadlet
    # Git repository containing Quadlet files
    url: https://github.com/myorg/containers.git
    branch: main
    # Directory within repo containing Quadlet files
    targetPath: quadlet/webapp/
    # Cron schedule for polling (every 5 minutes)
    schedule: "*/5 * * * *"

    # Deployment method configuration
    method:
      # Type must be "quadlet"
      type: quadlet

      # Deploy in rootful mode (system-wide)
      # true: Uses /etc/containers/systemd/ (requires root)
      # false: Uses ~/.config/containers/systemd/ (user-level)
      root: true

      # Enable and start services after deployment
      enable: true

      # Restart services on updates
      # If true, services are restarted when Quadlet files change
      # If false, services are enabled but not restarted
      restart: false

      # Optional: Glob pattern to filter files
      # Default: "**" (all files)
      glob: "**/*.{container,volume,network}"
```

### Multi-Target Configuration Example

```yaml
targets:
  # Production deployment (rootful)
  - name: production-app
    url: https://github.com/myorg/prod-containers.git
    branch: main
    targetPath: quadlet/
    schedule: "*/10 * * * *"
    method:
      type: quadlet
      root: true
      enable: true
      restart: true

  # Development deployment (rootless)
  - name: dev-app
    url: https://github.com/myorg/dev-containers.git
    branch: develop
    targetPath: quadlet/
    schedule: "*/2 * * * *"
    method:
      type: quadlet
      root: false  # Rootless (user-level)
      enable: true
      restart: true

  # Database volumes and networks only (no restart)
  - name: database-infrastructure
    url: https://github.com/myorg/infra-containers.git
    branch: main
    targetPath: quadlet/database/
    schedule: "*/30 * * * *"
    method:
      type: quadlet
      root: true
      enable: true
      restart: false  # Don't restart on updates (volumes/networks)
      glob: "**/*.{volume,network}"
```

---

## 6. Interface Implementation

### Method Interface Requirements

The Quadlet struct must implement the Method interface:

```go
type Method interface {
    // GetKind returns the method type ("quadlet")
    GetKind() string

    // Process handles the periodic polling and change detection
    Process(ctx, conn context.Context, skew int)

    // MethodEngine handles individual file changes
    MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error

    // Apply processes all changes between two Git states
    Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error
}
```

### Quadlet Implementation Signatures

```go
// GetKind returns "quadlet"
func (q *Quadlet) GetKind() string {
    return "quadlet"
}

// Process is called periodically based on the schedule
// Handles initial clone or fetch updates, detects changes, and applies them
func (q *Quadlet) Process(ctx, conn context.Context, skew int) {
    // Implementation will:
    // 1. Sleep for skew milliseconds to distribute load
    // 2. Lock the target mutex
    // 3. Clone or fetch Git repository
    // 4. Detect file changes
    // 5. Call Apply() with current and desired states
}

// MethodEngine handles a single file change
func (q *Quadlet) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error {
    // Implementation will:
    // 1. Determine change type (create/update/rename/delete)
    // 2. Get Quadlet directory paths
    // 3. Perform file operations (copy, move, delete)
    // 4. Return nil (daemon-reload handled in batch by Apply())
}

// Apply processes all changes in a batch and triggers daemon-reload
func (q *Quadlet) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error {
    // Implementation will:
    // 1. Call applyChanges() to get filtered change map
    // 2. Call runChanges() to process each change via MethodEngine()
    // 3. Trigger systemd daemon-reload (once for all changes)
    // 4. Optionally enable/start services
    // 5. Verify service generation
    // 6. Return errors if any step fails
}
```

---

## 7. Error Types

### Quadlet-Specific Errors

```go
// QuadletError represents errors specific to Quadlet operations
type QuadletError struct {
    Operation string // "file_placement", "daemon_reload", "service_start", etc.
    Target    string // Target name
    Service   string // Service name (if applicable)
    Err       error  // Underlying error
}

func (e *QuadletError) Error() string {
    if e.Service != "" {
        return fmt.Sprintf("quadlet %s failed for target %s, service %s: %v",
            e.Operation, e.Target, e.Service, e.Err)
    }
    return fmt.Sprintf("quadlet %s failed for target %s: %v",
        e.Operation, e.Target, e.Err)
}

// Common error scenarios
var (
    ErrHomeNotSet        = errors.New("HOME environment variable not set (required for rootless)")
    ErrXDGRuntimeNotSet  = errors.New("XDG_RUNTIME_DIR not set (required for rootless)")
    ErrDirectoryCreate   = errors.New("failed to create Quadlet directory")
    ErrDaemonReload      = errors.New("systemd daemon-reload failed")
    ErrServiceNotFound   = errors.New("service not generated by Quadlet (check file syntax)")
    ErrInvalidFileType   = errors.New("unsupported Quadlet file type")
)
```

---

## 8. Service Name Derivation

### Naming Conventions

Quadlet filenames map to systemd service names following specific conventions:

```go
// DeriveServiceName converts a Quadlet filename to a systemd service name
func DeriveServiceName(quadletFilename string) string {
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

// DerivePodmanResourceName converts a Quadlet filename to Podman resource name
func DerivePodmanResourceName(quadletFilename string) string {
    ext := filepath.Ext(quadletFilename)
    base := strings.TrimSuffix(filepath.Base(quadletFilename), ext)

    // Podman resources are prefixed with "systemd-" by default
    return "systemd-" + base
}
```

**Examples**:

| Quadlet File | systemd Service | Podman Resource |
|--------------|----------------|-----------------|
| `nginx.container` | `nginx.service` | `systemd-nginx` |
| `db-data.volume` | `db-data-volume.service` | `systemd-db-data` |
| `app-network.network` | `app-network-network.service` | `systemd-app-network` |
| `webapp.kube` | `webapp.service` | `systemd-webapp` |

---

## 9. Dependencies Between Resources

### Automatic Dependency Resolution

Quadlet automatically creates dependencies when files reference each other:

```go
// DependencyReference represents a reference to another Quadlet resource
type DependencyReference struct {
    // Type is the dependency type (volume, network)
    Type QuadletFileType

    // Name is the base name of the referenced file (without extension)
    Name string

    // ServiceName is the derived systemd service name
    ServiceName string
}

// ExtractDependencies analyzes a Quadlet file content for dependencies
// Example: If myapp.container contains "Volume=data.volume:/path"
// This returns a DependencyReference for data-volume.service
func ExtractDependencies(quadletContent string, fileType QuadletFileType) []DependencyReference {
    // Implementation would parse the file content and look for:
    // - Volume= lines ending in .volume
    // - Network= lines ending in .network
    // Return list of dependencies
    // Quadlet's generator will automatically add Requires= and After= directives
}
```

**Dependency Chain Example**:

```
app-network.network (creates app-network-network.service)
     ↓ (referenced by)
db-data.volume (creates db-data-volume.service)
     ↓ (referenced by)
postgres.container (creates postgres.service)
     ↓ (required by - manual systemd directive)
webapp.container (creates webapp.service)
```

systemd ensures services start in order:
1. `app-network-network.service`
2. `db-data-volume.service`
3. `postgres.service`
4. `webapp.service`

---

## 10. Validation Rules

### File Validation

```go
// QuadletFileValidation defines validation rules for Quadlet files
type QuadletFileValidation struct {
    // MinPodmanVersion is the minimum required Podman version
    MinPodmanVersion string // "4.4.0"

    // MaxFileSizeBytes is the maximum file size
    MaxFileSizeBytes int64 // 1MB typical limit

    // RequiredSections are mandatory INI sections
    RequiredSections map[QuadletFileType][]string

    // AllowedExtensions are valid file extensions
    AllowedExtensions []string // .container, .volume, .network, .kube
}

// Example validation rules
var DefaultValidation = QuadletFileValidation{
    MinPodmanVersion: "4.4.0",
    MaxFileSizeBytes: 1 * 1024 * 1024, // 1MB

    RequiredSections: map[QuadletFileType][]string{
        QuadletContainer: {"Container"}, // [Container] section required
        QuadletVolume:    {},             // No required sections for volumes
        QuadletNetwork:   {},             // No required sections for networks
        QuadletKube:      {"Kube"},       // [Kube] section required
    },

    AllowedExtensions: []string{".container", ".volume", ".network", ".kube"},
}

// ValidateQuadletFile performs basic validation on a Quadlet file
func ValidateQuadletFile(filePath string, content []byte) error {
    // Check file extension
    // Check file size
    // Verify required sections present
    // Optionally parse INI format
    // Return validation errors
}
```

---

## 11. Observability and Logging

### Logging Context

```go
// QuadletLogContext provides structured logging context for Quadlet operations
type QuadletLogContext struct {
    Target      string // Target name
    Operation   string // "file_placement", "daemon_reload", "service_start"
    File        string // Quadlet file path
    Service     string // Systemd service name
    Root        bool   // Rootful or rootless
    ChangeType  string // create, update, rename, delete
}

// Example logging calls
logger.With("context", QuadletLogContext{
    Target:     "webapp-quadlet",
    Operation:  "file_placement",
    File:       "nginx.container",
    Service:    "nginx.service",
    Root:       true,
    ChangeType: "create",
}).Infof("Placing Quadlet file in /etc/containers/systemd/")

logger.With("context", QuadletLogContext{
    Target:    "webapp-quadlet",
    Operation: "daemon_reload",
    Root:      true,
}).Infof("Triggering systemd daemon-reload")
```

---

## 12. Performance Considerations

### Batching Strategy

```go
// BatchOperation represents a batch of Quadlet operations
type BatchOperation struct {
    // Changes is the list of file changes to process
    Changes []*object.Change

    // RequiresDaemonReload indicates if daemon-reload is needed
    RequiresDaemonReload bool

    // ServicesToEnable is the list of services to enable after reload
    ServicesToEnable []string

    // ServicesToStart is the list of services to start after enable
    ServicesToStart []string
}

// ProcessBatch processes all changes in a single batch
func (q *Quadlet) ProcessBatch(ctx context.Context, batch BatchOperation) error {
    // 1. Process all file changes (create, update, delete)
    for _, change := range batch.Changes {
        if err := q.MethodEngine(ctx, nil, change, ""); err != nil {
            return err
        }
    }

    // 2. Single daemon-reload after all file changes
    if batch.RequiresDaemonReload {
        if err := systemdDaemonReload(ctx, q.Root); err != nil {
            return err
        }
    }

    // 3. Enable services
    if q.Enable {
        for _, service := range batch.ServicesToEnable {
            if err := systemdEnableService(ctx, service, q.Root); err != nil {
                return err
            }
        }
    }

    // 4. Start services
    for _, service := range batch.ServicesToStart {
        if err := systemdStartService(ctx, service, q.Root); err != nil {
            return err
        }
    }

    return nil
}
```

**Rationale**: daemon-reload is expensive (reruns all generators, reloads all units). Batching file changes and triggering a single daemon-reload significantly improves performance.

---

## Summary

This data model provides:

1. **Clear structure** - Quadlet struct follows existing Method pattern
2. **Configuration flexibility** - Supports rootful, rootless, enable, restart options
3. **Type safety** - Enums for file types and change types
4. **State tracking** - Deployment status and service states
5. **Error handling** - Quadlet-specific error types
6. **Validation rules** - File size, extensions, required sections
7. **Performance optimization** - Batch operations and single daemon-reload
8. **Observability** - Structured logging context

All data structures are designed to integrate seamlessly with fetchit's existing engine framework while supporting Quadlet-specific requirements.
