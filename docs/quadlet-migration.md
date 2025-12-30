# Migrating from systemd Method to Quadlet

**Last Updated**: 2025-12-30
**Applies To**: fetchit users currently using the `systemd` deployment method

## Table of Contents

1. [Overview](#overview)
2. [Why Migrate?](#why-migrate)
3. [Comparison: systemd vs Quadlet](#comparison-systemd-vs-quadlet)
4. [Prerequisites](#prerequisites)
5. [Step-by-Step Migration](#step-by-step-migration)
6. [Conversion Examples](#conversion-examples)
7. [Migration Checklist](#migration-checklist)
8. [Rollback Procedures](#rollback-procedures)
9. [Troubleshooting](#troubleshooting)

---

## Overview

This guide helps you migrate existing fetch it deployments from the legacy `systemd` method to the modern `quadlet` method. The migration is straightforward and can be done with minimal disruption.

**Migration Path**:
```
systemd method → quadlet method
(uses helper container) → (native Podman integration)
```

---

## Why Migrate?

### Benefits of Quadlet

1. **No Helper Container Required**
   - systemd method: Requires `quay.io/fetchit/fetchit-systemd` helper container
   - Quadlet: Direct Podman integration, no additional containers

2. **Simpler Configuration**
   - systemd: Complex service files with `ExecStart`, `ExecStop`, cleanup logic
   - Quadlet: Declarative `.container` files, easier to read and maintain

3. **Better Performance**
   - systemd: Overhead of running helper container for each systemctl operation
   - Quadlet: Direct systemd integration via D-Bus

4. **Native Podman Feature**
   - Quadlet is built into Podman (v4.4+), maintained by the Podman team
   - Future-proof your deployments

---

## Comparison: systemd vs Quadlet

| Feature | systemd Method | Quadlet Method |
|---------|---------------|----------------|
| **Helper Container** | Required (`fetchit-systemd`) | None |
| **File Format** | `.service` files | `.container`, `.volume`, `.network`, `.kube` files |
| **Complexity** | High (bash scripts, ExecStart/Stop) | Low (declarative) |
| **Directory** | `/etc/systemd/system/` | `/etc/containers/systemd/` |
| **File Placement** | Via helper container | Direct file copy |
| **daemon-reload** | Via systemctl in container | D-Bus API |
| **Dependencies** | Helper container image | None (native Podman) |
| **Podman Version** | Any | 4.4+ (for Quadlet) |

---

## Prerequisites

### System Requirements

1. **Podman 4.4 or later** (Quadlet is integrated starting with v4.4)
   ```bash
   podman --version
   # Should show: podman version 4.4.0 or later
   ```

2. **systemd** (already required for systemd method)
   ```bash
   systemctl --version
   ```

3. **For rootless**: Lingering enabled
   ```bash
   sudo loginctl enable-linger $USER
   ```

### Environment Check

```bash
# Rootful deployments
sudo mkdir -p /etc/containers/systemd
sudo chmod 755 /etc/containers/systemd

# Rootless deployments
mkdir -p ~/.config/containers/systemd
chmod 755 ~/.config/containers/systemd

# Verify XDG_RUNTIME_DIR is set (rootless only)
echo $XDG_RUNTIME_DIR
# Should output: /run/user/<UID>
```

---

## Step-by-Step Migration

### Step 1: Identify Current Deployments

List your current systemd-based targets:

```bash
# Check your fetchit configuration
cat /opt/mount/config.yaml | grep -A 20 "systemd:"
```

Example current configuration:
```yaml
targets:
  - name: webapp
    url: https://github.com/myorg/services.git
    branch: main
    targetPath: systemd/
    schedule: "*/5 * * * *"
    method:
      type: systemd
      root: true
      enable: true
```

### Step 2: Convert systemd Service Files to Quadlet

See [Conversion Examples](#conversion-examples) below for detailed examples.

**General conversion pattern**:

**systemd service (`httpd.service`)**:
```ini
[Unit]
Description=Apache web server

[Service]
ExecStartPre=/usr/bin/podman rm -f httpd
ExecStart=/usr/bin/podman run \
    --name httpd \
    -p 8080:80 \
    -v /var/www:/usr/local/apache2/htdocs:Z \
    docker.io/library/httpd:latest
ExecStop=/usr/bin/podman stop httpd
Restart=always

[Install]
WantedBy=multi-user.target
```

**Quadlet file (`httpd.container`)**:
```ini
[Unit]
Description=Apache web server

[Container]
Image=docker.io/library/httpd:latest
ContainerName=httpd
PublishPort=8080:80
Volume=/var/www:/usr/local/apache2/htdocs:Z

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**Key differences**:
- Remove `ExecStart`, `ExecStop`, `ExecStartPre`
- Move container configuration to `[Container]` section
- No need for `--rm -f` logic (Quadlet handles cleanup)
- Simpler, more declarative syntax

### Step 3: Create New Directory in Git Repository

```bash
# In your services repository
mkdir quadlet/
cp systemd/*.service quadlet/  # Copy for conversion
cd quadlet/

# Rename files
for f in *.service; do mv "$f" "${f%.service}.container"; done

# Edit each file to convert syntax (see examples below)
```

### Step 4: Update fetchit Configuration

Create a new quadlet target alongside your existing systemd target:

```yaml
targets:
  # Keep existing systemd target (for rollback)
  - name: webapp-systemd
    url: https://github.com/myorg/services.git
    branch: main
    targetPath: systemd/
    schedule: "*/5 * * * *"
    method:
      type: systemd
      root: true
      enable: true

  # Add new quadlet target
  - name: webapp-quadlet
    url: https://github.com/myorg/services.git
    branch: main
    targetPath: quadlet/  # New directory
    schedule: "*/5 * * * *"
    method:
      type: quadlet
      root: true
      enable: true
      restart: false  # Don't restart on first deployment
```

### Step 5: Test Migration

1. **Commit quadlet files** to your repository
2. **Restart fetchit** to pick up new configuration
3. **Monitor deployment**:

```bash
# Watch for Quadlet files
watch -n 2 'ls -la /etc/containers/systemd/'

# Watch for service generation
watch -n 2 'systemctl list-units | grep httpd'

# Check fetchit logs
podman logs -f fetchit
```

4. **Verify services are running**:

```bash
# Check service status
systemctl status httpd.service

# Verify container
podman ps | grep httpd

# Test application
curl http://localhost:8080
```

### Step 6: Remove Old systemd Target

Once confident the Quadlet deployment works:

1. **Stop old systemd services** (if running)
2. **Remove systemd target** from fetchit config
3. **Commit changes**
4. **Clean up old files** (optional)

```yaml
targets:
  # Remove this:
  # - name: webapp-systemd
  #   method:
  #     type: systemd

  # Keep this:
  - name: webapp-quadlet
    method:
      type: quadlet
```

---

## Conversion Examples

### Example 1: Simple Web Server

**Before** (`httpd.service`):
```ini
[Unit]
Description=Apache HTTP Server

[Service]
ExecStartPre=/usr/bin/podman rm -f httpd
ExecStart=/usr/bin/podman run --name httpd -p 8080:80 docker.io/library/httpd:latest
ExecStop=/usr/bin/podman stop httpd
Restart=always

[Install]
WantedBy=multi-user.target
```

**After** (`httpd.container`):
```ini
[Unit]
Description=Apache HTTP Server

[Container]
Image=docker.io/library/httpd:latest
ContainerName=httpd
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

### Example 2: Container with Volume and Environment Variables

**Before** (`postgres.service`):
```ini
[Unit]
Description=PostgreSQL Database

[Service]
ExecStartPre=/usr/bin/podman rm -f postgres
ExecStart=/usr/bin/podman run \
    --name postgres \
    -p 5432:5432 \
    -v postgres-data:/var/lib/postgresql/data:Z \
    -e POSTGRES_PASSWORD=secret \
    -e POSTGRES_DB=myapp \
    docker.io/library/postgres:15
ExecStop=/usr/bin/podman stop postgres
Restart=always

[Install]
WantedBy=multi-user.target
```

**After** (two files):

**`postgres-data.volume`**:
```ini
[Volume]
Label=app=postgres

[Install]
WantedBy=multi-user.target
```

**`postgres.container`**:
```ini
[Unit]
Description=PostgreSQL Database

[Container]
Image=docker.io/library/postgres:15
ContainerName=postgres
PublishPort=5432:5432
Volume=postgres-data.volume:/var/lib/postgresql/data:Z
Environment=POSTGRES_PASSWORD=secret
Environment=POSTGRES_DB=myapp

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**Key changes**:
- Volume definition moved to separate `.volume` file
- Environment variables: one `Environment=` line per variable
- Volume reference: `postgres-data.volume:/path` (Quadlet creates dependency)

### Example 3: Multi-Container Application

**Before** (single complex service file):
```ini
# Not recommended with systemd method
```

**After** (three Quadlet files):

**`app-network.network`**:
```ini
[Network]
Subnet=172.20.0.0/16
Label=app=webapp

[Install]
WantedBy=multi-user.target
```

**`postgres.container`**:
```ini
[Unit]
Description=PostgreSQL Database

[Container]
Image=docker.io/library/postgres:15
Network=app-network.network
Environment=POSTGRES_PASSWORD=secret

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**`webapp.container`**:
```ini
[Unit]
Description=Web Application
After=postgres.service
Requires=postgres.service

[Container]
Image=docker.io/myapp:latest
Network=app-network.network
PublishPort=3000:3000
Environment=DATABASE_URL=postgresql://systemd-postgres:5432/myapp

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**Benefits**:
- Clear separation of concerns
- Automatic dependency management
- Easier to understand and maintain

---

## Migration Checklist

Use this checklist to ensure a smooth migration:

### Pre-Migration
- [ ] Verify Podman version is 4.4 or later
- [ ] Enable lingering for rootless deployments
- [ ] Backup current fetchit configuration
- [ ] Document current service names and container names
- [ ] Test Quadlet files locally before committing

### Conversion
- [ ] Create `quadlet/` directory in Git repository
- [ ] Convert all `.service` files to `.container` format
- [ ] Extract volumes to `.volume` files (if using named volumes)
- [ ] Extract networks to `.network` files (if using custom networks)
- [ ] Verify `[Install]` section has correct `WantedBy=` target
- [ ] Test each Quadlet file individually

### Testing
- [ ] Add new quadlet target to fetchit config
- [ ] Keep old systemd target active (for rollback)
- [ ] Commit Quadlet files to repository
- [ ] Restart fetchit with new configuration
- [ ] Wait for file placement in `/etc/containers/systemd/`
- [ ] Verify `systemctl daemon-reload` was triggered
- [ ] Check services are generated (`systemctl list-units`)
- [ ] Verify services are active (`systemctl is-active`)
- [ ] Test application functionality
- [ ] Monitor logs for 24 hours

### Cleanup
- [ ] Remove old systemd target from config
- [ ] Stop and disable old systemd services
- [ ] Remove old `.service` files from repository (optional)
- [ ] Update documentation to reference Quadlet
- [ ] Celebrate successful migration!

---

## Rollback Procedures

If something goes wrong, you can easily rollback:

### Option 1: Disable Quadlet Target

```yaml
# In fetchit config, comment out or remove:
# targets:
#   - name: webapp-quadlet
#     method:
#       type: quadlet

# Keep systemd target active:
targets:
  - name: webapp-systemd
    method:
      type: systemd
```

### Option 2: Stop Quadlet Services Manually

```bash
# Rootful
sudo systemctl stop httpd.service
sudo systemctl disable httpd.service

# Rootless
systemctl --user stop httpd.service
systemctl --user disable httpd.service

# Remove Quadlet files
sudo rm /etc/containers/systemd/*.container
sudo systemctl daemon-reload
```

---

## Troubleshooting

### Service Not Generated After daemon-reload

**Symptom**: Quadlet file placed but service not found

**Solution**:
```bash
# Check Quadlet file syntax
cat /etc/containers/systemd/httpd.container

# Verify required [Container] section exists
# Verify Image= directive is present

# Check systemd generator logs
journalctl -xe | grep -i quadlet

# Manually test generator
/usr/lib/systemd/system-generators/podman-system-generator --dryrun
```

### Permission Denied (Rootless)

**Symptom**: Cannot write to `~/.config/containers/systemd/`

**Solution**:
```bash
# Ensure directory exists and has correct permissions
mkdir -p ~/.config/containers/systemd
chmod 755 ~/.config/containers/systemd

# Verify XDG_RUNTIME_DIR is set
echo $XDG_RUNTIME_DIR
# If not set:
export XDG_RUNTIME_DIR=/run/user/$(id -u)
```

### Container Name Conflicts

**Symptom**: Error about container name already in use

**Solution**:
```bash
# Quadlet uses "systemd-<basename>" by default
# If your .container file is named "httpd.container":
# - Container name will be: systemd-httpd
# - Service name will be: httpd.service

# To use a custom name, add to [Container] section:
ContainerName=my-custom-name
```

### Image Pull Timeout

**Symptom**: Service fails to start with timeout

**Solution**:
```ini
# Increase timeout in Quadlet file
[Service]
TimeoutStartSec=300  # 5 minutes
```

### Services Don't Start on Boot

**Symptom**: Services don't auto-start after reboot

**Solution**:
```bash
# Verify [Install] section
# For rootful:
WantedBy=multi-user.target

# For rootless:
WantedBy=default.target

# Enable lingering (rootless only):
sudo loginctl enable-linger $USER
```

---

## Further Reading

- [Quadlet Quickstart Guide](../specs/002-quadlet-support/quickstart.md)
- [Podman Quadlet Official Docs](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Quadlet File Format Reference](../QUADLET-REFERENCE.md)
- [fetchit Methods Documentation](./methods.rst)

---

## Support

If you encounter issues during migration:

1. **Check fetchit logs**: `podman logs fetchit`
2. **Check systemd journals**: `journalctl -xe`
3. **Verify Quadlet file syntax**: Compare with examples in `examples/quadlet/`
4. **Open an issue**: [GitHub Issues](https://github.com/containers/fetchit/issues)

---

**Happy migrating!** The Quadlet method provides a simpler, more maintainable approach to container deployments with fetchit.
