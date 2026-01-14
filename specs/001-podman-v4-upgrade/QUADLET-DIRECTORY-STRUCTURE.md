# Podman Quadlet Directory Structure and Permission Requirements

## Executive Summary

This document provides comprehensive information about Quadlet directory structures, permission requirements, and edge case handling for implementing file placement logic in Linux systemd integration with Podman.

## 1. Directory Paths for Rootful Deployments

Quadlet files for rootful (system-level) containers follow a hierarchical precedence order. Files in directories with higher precedence override those in lower precedence directories.

### 1.1 Rootful Directory Search Paths (Precedence Order)

| Priority | Path | Purpose | Precedence Level |
|----------|------|---------|------------------|
| 1 (Highest) | `/run/containers/systemd/` | Temporary quadlets for testing | Takes precedence over `/etc` and `/usr` |
| 2 | `/etc/containers/systemd/` | System administrator-defined quadlets (recommended) | Takes precedence over `/usr` |
| 3 (Lowest) | `/usr/share/containers/systemd/` | Distribution-defined quadlets | Lowest precedence |

### 1.2 Rootful File Extensions

Quadlet supports the following file extensions in these directories:
- `.container` - Container unit definitions
- `.volume` - Volume definitions
- `.network` - Network definitions
- `.kube` - Kubernetes YAML deployment
- `.image` - Image pull/build definitions
- `.build` - Build definitions
- `.pod` - Pod definitions

### 1.3 Rootful Subdirectory Support

Quadlet supports placing unit files in subdirectories within the root search paths, allowing for organized file structure.

## 2. Directory Paths for Rootless Deployments

Rootless deployments have multiple search paths that cater to different use cases (user-specific, UID-specific, and system-wide user configurations).

### 2.1 Rootless Directory Search Paths

| Priority | Path | Purpose | Notes |
|----------|------|---------|-------|
| 1 | `$XDG_RUNTIME_DIR/containers/systemd/` | Runtime-specific quadlets | Typically `/run/user/${UID}/containers/systemd/` |
| 2 | `$XDG_CONFIG_HOME/containers/systemd/` | User configuration quadlets | Falls back to `~/.config/containers/systemd/` |
| 3 | `~/.config/containers/systemd/` | User configuration (standard location) | **Most commonly recommended** |
| 4 | `/etc/containers/systemd/users/${UID}/` | UID-specific quadlets managed by admin | Executes only for matching UID |
| 5 | `/etc/containers/systemd/users/` | All-users quadlets managed by admin | Executes for all user sessions |

### 2.2 Rootless Subdirectory Support

Similar to rootful, rootless Quadlet supports placing unit files in subdirectories within any of the search paths, allowing organizational flexibility.

**Known Issue**: As of recent versions, there are reported bugs where subdirectories within `/etc/containers/systemd/users/` may not be properly scanned (Issue #24783).

### 2.3 XDG_RUNTIME_DIR Handling

#### What is XDG_RUNTIME_DIR?

`XDG_RUNTIME_DIR` is an environment variable that points to a user-specific runtime directory, typically `/run/user/${UID}`. This directory:
- Is created automatically by systemd when a user logs in
- Has permissions `0700` (accessible only by the owning user)
- Is cleaned up when all user sessions end
- Contains ephemeral runtime data

#### Standard XDG_RUNTIME_DIR Path

For a user with UID 1000:
```bash
XDG_RUNTIME_DIR=/run/user/1000
```

#### Checking XDG_RUNTIME_DIR

```bash
echo $XDG_RUNTIME_DIR
# If empty, it should be set to:
export XDG_RUNTIME_DIR=/run/user/$(id -u)
```

#### Common XDG_RUNTIME_DIR Issues

**Issue 1: User Switching Methods**

When using `su` to switch users, the previous user's environment is retained, leading to permission errors:

```
ERRO[0000] XDG_RUNTIME_DIR directory "/run/user/1000" is not owned by the current user
```

**Solutions**:
- Use `su -` or `su - username` (with dash) to properly initialize the user environment
- Use `sudo -u username -s` instead of plain `su`
- Use `machinectl` for user switching
- SSH to localhost and then run rootless containers

**Issue 2: Missing or Invalid systemd Session**

Rootless containers require a valid systemd session with proper cgroup configuration.

**Solution**: Enable lingering for the user to maintain their session even when not logged in:
```bash
loginctl enable-linger $USER
```

**Note**: After enabling lingering, a host reboot may be required for Podman to work correctly as a lingering user.

**Issue 3: Quadlet Generator Path Issues**

The Podman Quadlet generator uses `%t` in generated systemd units. When a system service uses `User=`, `%t` resolves to `/run` where unprivileged users lack permissions.

**Workaround**: The generator should use `$XDG_RUNTIME_DIR` instead, but this is a known limitation (Issue #26671).

## 3. Permission Requirements

### 3.1 Directory Permissions

| Directory Type | Path | Owner | Permissions | Notes |
|----------------|------|-------|-------------|-------|
| Rootful | `/etc/containers/systemd/` | `root:root` | `0755` (drwxr-xr-x) | Must be created with `sudo` |
| Rootful | `/run/containers/systemd/` | `root:root` | `0755` (drwxr-xr-x) | Temporary, lost on reboot |
| Rootless | `~/.config/containers/systemd/` | `user:user` | `0700` or `0755` | Podman creates with `0700` |
| Rootless | `$XDG_RUNTIME_DIR/containers/systemd/` | `user:user` | `0700` | Part of runtime dir |
| Admin-managed | `/etc/containers/systemd/users/${UID}/` | `root:root` | `0755` | System-managed user quadlets |

**Note on ~/.config Permissions**: Podman creates `~/.config` with `0700` permissions (more restrictive), while systemd typically uses `0755`. This is intentional for security.

### 3.2 File Permissions

All Quadlet unit files (`.container`, `.volume`, `.network`, `.kube`, `.image`, `.build`, `.pod`) should use standard systemd unit file permissions:

| File Type | Owner | Permissions | Rationale |
|-----------|-------|-------------|-----------|
| Rootful Quadlet files | `root:root` | `0644` (-rw-r--r--) | Root modifies, systemd reads |
| Rootless Quadlet files | `user:user` | `0644` (-rw-r--r--) | User modifies, systemd reads |

**Why 0644?**
- Owner (root or user) can read and write
- Group members can only read
- Others can only read
- Files are configuration, not executables (no need for `0755`)

**Setting Permissions**:
```bash
# Rootful
sudo chmod 0644 /etc/containers/systemd/myapp.container
sudo chown root:root /etc/containers/systemd/myapp.container

# Rootless
chmod 0644 ~/.config/containers/systemd/myapp.container
```

### 3.3 Permission Verification

Systemd will typically warn about incorrect permissions on unit files. Monitor logs:

```bash
# System logs
journalctl -xe

# User logs
journalctl --user -xe
```

## 4. Directory Creation Requirements

### 4.1 Do Directories Need to be Created?

**Yes** - Quadlet directories do **not** exist by default and **must be created** before placing Quadlet files.

### 4.2 Creating Rootful Directories

```bash
# Recommended location for system administrators
sudo mkdir -p /etc/containers/systemd

# Optional: Testing location (lost on reboot)
sudo mkdir -p /run/containers/systemd

# Set proper permissions
sudo chmod 0755 /etc/containers/systemd
sudo chown root:root /etc/containers/systemd
```

### 4.3 Creating Rootless Directories

```bash
# Standard user location (recommended)
mkdir -p ~/.config/containers/systemd

# Permissions are automatically set correctly by mkdir
# Podman will create ~/.config with 0700 if it doesn't exist

# Optional: Verify permissions
ls -ld ~/.config/containers/systemd
```

### 4.4 Creating Subdirectories

Both rootful and rootless support subdirectories for organization:

```bash
# Rootful
sudo mkdir -p /etc/containers/systemd/web-services
sudo chmod 0755 /etc/containers/systemd/web-services

# Rootless
mkdir -p ~/.config/containers/systemd/web-services
```

### 4.5 Programmatic Directory Creation

When creating directories programmatically (e.g., in Go, Python, or shell scripts):

```bash
# Bash
mkdir -p -m 0755 /etc/containers/systemd

# With ownership (rootful only)
install -d -m 0755 -o root -g root /etc/containers/systemd
```

**Go Example**:
```go
import "os"

// Rootful
err := os.MkdirAll("/etc/containers/systemd", 0755)

// Rootless
homeDir := os.Getenv("HOME")
path := filepath.Join(homeDir, ".config", "containers", "systemd")
err := os.MkdirAll(path, 0755)
```

**Ansible Example**:
```yaml
- name: Create Quadlet directory
  file:
    path: /etc/containers/systemd
    state: directory
    mode: '0755'
    owner: root
    group: root
    recurse: yes
```

## 5. Systemd Integration

### 5.1 Generator Locations

Quadlet uses systemd generators to convert Quadlet files into systemd service units:

| Type | Generator Path | Generated Output Path |
|------|---------------|----------------------|
| System (rootful) | `/usr/lib/systemd/system-generators/podman-system-generator` | `/run/systemd/generator/` |
| User (rootless) | `/usr/lib/systemd/user-generators/podman-user-generator` | `${XDG_RUNTIME_DIR}/systemd/generator/` (typically `/run/user/${UID}/systemd/generator/`) |

**Important**: Generated files are written to `/run/` directories and are **deleted on system reboot**. The generators recreate them on boot or when `systemctl daemon-reload` is executed.

### 5.2 Daemon Reload Requirement

After placing or modifying Quadlet files, you **must** run daemon-reload for systemd to discover and generate the service units:

```bash
# Rootful
sudo systemctl daemon-reload

# Rootless
systemctl --user daemon-reload
```

Without daemon-reload:
- New Quadlet files won't be discovered
- Modified Quadlet files won't be updated
- Services won't appear in `systemctl list-units`

### 5.3 Automatic Boot-time Generation

Systemd automatically runs the Quadlet generators:
- During system boot (rootful)
- During user session start (rootless)
- When `systemctl daemon-reload` is executed

## 6. Edge Cases and Special Scenarios

### 6.1 Symbolic Links

Quadlet supports symbolic links in all search paths. This allows for:
- Linking Quadlet files from other locations
- Using version control to manage Quadlet files elsewhere
- Creating aliases for Quadlet files

**Known Issue**: When `/etc/containers/systemd` is a symlink, units in `/etc/containers/systemd/users/${UID}` may be incorrectly loaded for the root user (Issue #23483).

### 6.2 Duplicate Named Quadlets

When the same filename exists in multiple search paths:
- Higher precedence directories override lower ones
- For rootful: `/run` > `/etc` > `/usr`
- For rootless: `$XDG_RUNTIME_DIR` > `$XDG_CONFIG_HOME` > `/etc/containers/systemd/users/${UID}` > `/etc/containers/systemd/users/`

### 6.3 User-Specific vs All-Users Quadlets

Administrators can control who executes Quadlets:

| Location | Execution Scope |
|----------|----------------|
| `/etc/containers/systemd/users/${UID}/` | Only user with matching UID |
| `/etc/containers/systemd/users/` | All users when their login session begins |
| `~/.config/containers/systemd/` | Only the specific user who owns the directory |

### 6.4 Rootless Containers Without Login Session

To run rootless containers even when the user is not logged in:

```bash
# Enable lingering
loginctl enable-linger $USER

# Verify lingering is enabled
loginctl show-user $USER | grep Linger
# Expected output: Linger=yes

# Check if user session is running
loginctl list-sessions
```

**Note**: After enabling lingering, Podman may require a host reboot to function correctly.

### 6.5 Missing XDG_RUNTIME_DIR

If `XDG_RUNTIME_DIR` is not set, rootless Podman will fail. Fix:

```bash
# Temporary fix (current session only)
export XDG_RUNTIME_DIR=/run/user/$(id -u)

# Permanent fix: Ensure systemd login session
# Use loginctl enable-linger to maintain user session
loginctl enable-linger $USER
```

### 6.6 Containers Running as Different Users

When a rootful systemd service uses `User=` directive to run as a non-root user:

**Problem**: Quadlet-generated services use `%t` which resolves to `/run` for system services, but the non-root user lacks write permissions.

**Current Status**: This is a known issue (Issue #26671). The generator should use `XDG_RUNTIME_DIR` for user-scoped paths.

**Workaround**: Run as fully rootless instead of rootful-with-user-directive.

### 6.7 Directory and File Ownership Mismatches

**Scenario**: Files placed in `~/.config/containers/systemd/` with wrong ownership.

**Symptom**: Systemd fails to read or execute Quadlet files.

**Fix**:
```bash
# Fix ownership recursively
chown -R $USER:$USER ~/.config/containers/systemd/

# Fix permissions
chmod 0644 ~/.config/containers/systemd/*.container
```

### 6.8 SELinux Contexts (RHEL/Fedora/CentOS)

On SELinux-enabled systems, ensure proper contexts:

```bash
# Rootful
sudo restorecon -Rv /etc/containers/systemd

# Rootless
restorecon -Rv ~/.config/containers/systemd
```

Check contexts:
```bash
ls -Z /etc/containers/systemd
```

### 6.9 Podman Auto-Update Integration

For auto-updating containers managed by Quadlet:

1. Add the `io.containers.autoupdate=registry` label to your `.container` file
2. Enable the Podman auto-update timer:

```bash
# Rootful
sudo systemctl enable --now podman-auto-update.timer

# Rootless
systemctl --user enable --now podman-auto-update.timer
```

Location of auto-update systemd units:
- Service: `/usr/lib/systemd/system/podman-auto-update.service`
- Timer: `/usr/lib/systemd/system/podman-auto-update.timer`

## 7. Implementation Checklist for File Placement Logic

When implementing Quadlet file placement in code (such as in fetchit):

### 7.1 Pre-Placement Validation

- [ ] Determine if deployment is rootful or rootless
- [ ] Verify `$HOME` is set (required for rootless)
- [ ] For rootless: Verify or set `$XDG_RUNTIME_DIR`
- [ ] For rootless: Verify user has valid systemd session
- [ ] Check if lingering is required and enabled

### 7.2 Directory Creation

- [ ] Check if target directory exists
- [ ] If not, create directory with `mkdir -p`
- [ ] Set correct permissions (`0755` for directories)
- [ ] Set correct ownership (`root:root` for rootful, `user:user` for rootless)
- [ ] Support subdirectory creation if needed

### 7.3 File Placement

- [ ] Copy/write Quadlet file to target directory
- [ ] Set file permissions to `0644`
- [ ] Set file ownership (`root:root` for rootful, `user:user` for rootless)
- [ ] Validate file extension (`.container`, `.volume`, `.network`, etc.)
- [ ] Handle overwrites (backup existing files if needed)

### 7.4 Post-Placement Operations

- [ ] Execute `systemctl daemon-reload` (or `systemctl --user daemon-reload`)
- [ ] Optionally enable services: `systemctl enable <service>`
- [ ] Optionally start services: `systemctl start <service>`
- [ ] Verify service was generated: `systemctl list-units | grep <name>`
- [ ] Check for errors: `journalctl -xe` or `journalctl --user -xe`

### 7.5 Error Handling

- [ ] Handle missing `$HOME` gracefully
- [ ] Handle missing `$XDG_RUNTIME_DIR` gracefully
- [ ] Handle permission denied errors when creating directories
- [ ] Handle systemctl errors (service failed to start, etc.)
- [ ] Provide clear error messages indicating rootful vs rootless issues

## 8. Code Examples for fetchit Integration

### 8.1 Current fetchit Implementation Analysis

Based on `/Users/rcook/git/fetchit/pkg/engine/systemd.go`, the current implementation:

**Lines 156-165**: Determines destination path
```go
nonRootHomeDir := os.Getenv("HOME")
if nonRootHomeDir == "" {
    return fmt.Errorf("Could not determine $HOME for host, must set $HOME on host machine for non-root systemd method")
}
var dest string
if sd.Root {
    dest = systemdPathRoot  // /etc/systemd/system
} else {
    dest = filepath.Join(nonRootHomeDir, ".config", "systemd", "user")
}
```

**Issue**: The current code places files in `/etc/systemd/system` for rootful and `~/.config/systemd/user` for rootless. These are **systemd service file locations**, not **Quadlet input directories**.

### 8.2 Recommended Path Updates

For proper Quadlet support, update the destination paths:

```go
// Current (systemd service files)
const systemdPathRoot = "/etc/systemd/system"
dest := filepath.Join(nonRootHomeDir, ".config", "systemd", "user")

// Recommended (Quadlet input directories)
const quadletPathRoot = "/etc/containers/systemd"
dest := filepath.Join(nonRootHomeDir, ".config", "containers", "systemd")
```

### 8.3 Enhanced Implementation with Directory Creation

```go
func (sd *Systemd) determineDestinationPath() (string, error) {
    if sd.Root {
        return "/etc/containers/systemd", nil
    }

    // Rootless path
    homeDir := os.Getenv("HOME")
    if homeDir == "" {
        return "", fmt.Errorf("$HOME not set - required for rootless Quadlet deployment")
    }

    return filepath.Join(homeDir, ".config", "containers", "systemd"), nil
}

func (sd *Systemd) ensureDestinationDirectory(dest string) error {
    // Check if directory exists
    if _, err := os.Stat(dest); os.IsNotExist(err) {
        logger.Infof("Creating Quadlet directory: %s", dest)

        // Create directory with proper permissions
        if err := os.MkdirAll(dest, 0755); err != nil {
            return fmt.Errorf("failed to create directory %s: %w", dest, err)
        }

        // For rootful, ensure root ownership (if running as root)
        if sd.Root && os.Geteuid() == 0 {
            if err := os.Chown(dest, 0, 0); err != nil {
                logger.Warnf("Failed to set ownership on %s: %v", dest, err)
            }
        }
    }

    return nil
}

func (sd *Systemd) placeQuadletFile(sourcePath, destPath string) error {
    // Read source file
    content, err := os.ReadFile(sourcePath)
    if err != nil {
        return fmt.Errorf("failed to read source file %s: %w", sourcePath, err)
    }

    // Write to destination with correct permissions
    if err := os.WriteFile(destPath, content, 0644); err != nil {
        return fmt.Errorf("failed to write Quadlet file %s: %w", destPath, err)
    }

    logger.Infof("Placed Quadlet file: %s", destPath)
    return nil
}
```

### 8.4 XDG_RUNTIME_DIR Handling Enhancement

```go
func (sd *Systemd) validateRootlessEnvironment() error {
    if sd.Root {
        return nil // No validation needed for rootful
    }

    // Check HOME
    if os.Getenv("HOME") == "" {
        return fmt.Errorf("$HOME not set - required for rootless deployment")
    }

    // Check XDG_RUNTIME_DIR
    xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
    if xdgRuntimeDir == "" {
        // Attempt to set it
        uid := os.Getuid()
        xdgRuntimeDir = fmt.Sprintf("/run/user/%d", uid)

        // Verify the directory exists
        if _, err := os.Stat(xdgRuntimeDir); os.IsNotExist(err) {
            return fmt.Errorf("$XDG_RUNTIME_DIR not set and %s does not exist - ensure systemd user session is active", xdgRuntimeDir)
        }

        // Set the environment variable
        os.Setenv("XDG_RUNTIME_DIR", xdgRuntimeDir)
        logger.Infof("Set XDG_RUNTIME_DIR=%s", xdgRuntimeDir)
    }

    return nil
}
```

## 9. Testing Recommendations

### 9.1 Manual Testing

**Rootful Deployment**:
```bash
# 1. Create directory
sudo mkdir -p /etc/containers/systemd

# 2. Place a test Quadlet file
sudo tee /etc/containers/systemd/test.container > /dev/null <<EOF
[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=default.target
EOF

# 3. Set permissions
sudo chmod 0644 /etc/containers/systemd/test.container

# 4. Reload systemd
sudo systemctl daemon-reload

# 5. Verify service was generated
systemctl list-units | grep test

# 6. Start the service
sudo systemctl start test.service

# 7. Check status
sudo systemctl status test.service
```

**Rootless Deployment**:
```bash
# 1. Create directory
mkdir -p ~/.config/containers/systemd

# 2. Place a test Quadlet file
tee ~/.config/containers/systemd/test.container > /dev/null <<EOF
[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=default.target
EOF

# 3. Set permissions (usually automatic)
chmod 0644 ~/.config/containers/systemd/test.container

# 4. Reload systemd
systemctl --user daemon-reload

# 5. Verify service was generated
systemctl --user list-units | grep test

# 6. Start the service
systemctl --user start test.service

# 7. Check status
systemctl --user status test.service
```

### 9.2 Automated Testing Scenarios

1. **Directory doesn't exist** - Verify automatic creation
2. **Directory exists with wrong permissions** - Verify correction
3. **File with wrong permissions** - Verify correction
4. **Rootless without $HOME** - Verify error handling
5. **Rootless without $XDG_RUNTIME_DIR** - Verify fallback/error handling
6. **Symbolic links** - Verify follow-through
7. **Subdirectories** - Verify recursive creation
8. **Daemon reload failure** - Verify error handling
9. **Service enable/start failure** - Verify error handling
10. **SELinux contexts** - Verify restoration (if applicable)

## 10. References

### Official Documentation
- [Podman systemd.unit Documentation](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Oracle Podman Quadlets Guide](https://docs.oracle.com/en/operating-systems/oracle-linux/podman/quadlets.html)
- [Red Hat Quadlet Guide](https://www.redhat.com/en/blog/quadlet-podman)

### Tutorials and Guides
- [Quadlet: Running Podman containers under systemd](https://mo8it.com/blog/quadlet/)
- [How to run Podman containers under Systemd with Quadlet](https://linuxconfig.org/how-to-run-podman-containers-under-systemd-with-quadlet)
- [Podman Quadlets Guide](https://jaze.dev/guides/podman/podman-quadlets)
- [ADMIN Magazine: Podman Quadlets](https://www.admin-magazine.com/Archive/2025/85/Run-rootless-Podman-containers-as-systemd-services)

### Technical References
- [Systemd Unit File Permissions - Baeldung](https://www.baeldung.com/linux/systemd-unit-file-permissions)
- [Adventures with rootless Podman containers - kcore.org](https://kcore.org/2023/12/13/adventures-with-rootless-containers/)

### Known Issues
- [Issue #26671: Quadlet generator should avoid using %t if User= is set](https://github.com/containers/podman/issues/26671)
- [Issue #24783: Quadlets in subfolders not generated](https://github.com/containers/podman/issues/24783)
- [Issue #23483: Rootless unit in /etc/containers/systemd/users/$(UID) loaded for root when /etc/containers/systemd is a symlink](https://github.com/containers/podman/issues/23483)
- [Issue #13338: XDG_RUNTIME_DIR directory not owned by current user](https://github.com/containers/podman/issues/13338)

## 11. Summary

### Key Takeaways

1. **Quadlet directories must be created** - They don't exist by default
2. **Rootful paths differ from rootless paths**:
   - Rootful: `/etc/containers/systemd/` (recommended)
   - Rootless: `~/.config/containers/systemd/` (recommended)
3. **Directories use 0755, files use 0644 permissions**
4. **XDG_RUNTIME_DIR is critical for rootless** - Must be set and valid
5. **systemctl daemon-reload is mandatory** - After placing/modifying files
6. **User sessions require lingering** - For services to run when user not logged in
7. **Subdirectories are supported** - For organized file structure
8. **Higher precedence paths override lower** - Plan file placement accordingly
9. **Generated services are ephemeral** - Stored in `/run/`, regenerated on boot
10. **Current fetchit implementation needs updates** - Should target Quadlet directories, not systemd service directories

### Critical Path for Implementation

```
1. Validate environment (HOME, XDG_RUNTIME_DIR for rootless)
   ↓
2. Determine destination directory (rootful vs rootless)
   ↓
3. Create directory if not exists (mkdir -p with 0755)
   ↓
4. Place Quadlet file with correct permissions (0644)
   ↓
5. Run systemctl daemon-reload
   ↓
6. Optionally enable/start the service
   ↓
7. Verify and handle errors
```
