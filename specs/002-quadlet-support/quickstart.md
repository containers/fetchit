# Quadlet Quickstart Guide

**Feature**: Quadlet Container Deployment
**Date**: 2025-12-30
**Audience**: Fetchit users looking to deploy containers with Quadlet

## Table of Contents

1. [What is Quadlet?](#what-is-quadlet)
2. [Prerequisites](#prerequisites)
3. [Quick Start](#quick-start)
4. [Creating Your First Quadlet File](#creating-your-first-quadlet-file)
5. [Configuring Fetchit for Quadlet](#configuring-fetchit-for-quadlet)
6. [Verifying Deployment](#verifying-deployment)
7. [Multi-Container Applications](#multi-container-applications)
8. [Migrating from Systemd Method](#migrating-from-systemd-method)
9. [Troubleshooting](#troubleshooting)

---

## What is Quadlet?

Quadlet is a systemd generator integrated into Podman that converts declarative container configuration files into systemd service units. Instead of writing complex systemd service files, you write simple `.container`, `.volume`, `.network`, or `.kube` files that describe what you want to deploy.

**Benefits**:
- ✅ **Simpler syntax** than systemd service files
- ✅ **Native Podman integration** - no helper container required
- ✅ **Automatic systemd integration** - Podman generates services for you
- ✅ **Declarative** - focus on what you want, not how to achieve it
- ✅ **Dependency management** - automatic service ordering

---

## Prerequisites

### System Requirements

1. **Podman 4.4 or later** (Quadlet integrated starting with 4.4)
   ```bash
   podman --version
   # Output: podman version 5.7.0
   ```

2. **systemd** as the init system
   ```bash
   systemctl --version
   # Output: systemd 255 (or later)
   ```

3. **For rootless deployments**: User systemd instance enabled
   ```bash
   # Enable lingering (allows services to run when not logged in)
   sudo loginctl enable-linger $USER

   # Verify
   loginctl show-user $USER | grep Linger
   # Output: Linger=yes
   ```

### Environment Variables (Rootless)

```bash
# HOME must be set
echo $HOME
# Output: /home/yourusername

# XDG_RUNTIME_DIR should be set (usually automatic)
echo $XDG_RUNTIME_DIR
# Output: /run/user/1000

# If not set, set it manually
export XDG_RUNTIME_DIR=/run/user/$(id -u)
```

---

## Quick Start

### 1. Create a Simple Quadlet File

Create a file named `nginx.container`:

```ini
[Unit]
Description=Nginx web server

[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=default.target
```

### 2. Place File in Quadlet Directory

**For rootful (system-wide)**:
```bash
sudo mkdir -p /etc/containers/systemd
sudo cp nginx.container /etc/containers/systemd/
sudo chmod 644 /etc/containers/systemd/nginx.container
```

**For rootless (user-level)**:
```bash
mkdir -p ~/.config/containers/systemd
cp nginx.container ~/.config/containers/systemd/
chmod 644 ~/.config/containers/systemd/nginx.container
```

### 3. Reload systemd

**Rootful**:
```bash
sudo systemctl daemon-reload
```

**Rootless**:
```bash
systemctl --user daemon-reload
```

### 4. Start the Service

**Rootful**:
```bash
sudo systemctl start nginx.service
sudo systemctl status nginx.service
```

**Rootless**:
```bash
systemctl --user start nginx.service
systemctl --user status nginx.service
```

### 5. Verify Container is Running

```bash
podman ps
# You should see a container named "systemd-nginx"
```

### 6. Test the Web Server

```bash
curl http://localhost:8080
# You should see the Nginx welcome page
```

---

## Creating Your First Quadlet File

### Container File Structure

A `.container` file has several sections:

#### [Unit] Section (Optional)

Standard systemd unit options:

```ini
[Unit]
Description=My application
Documentation=https://example.com/docs
After=network-online.target
Wants=network-online.target
```

#### [Container] Section (Required)

Container-specific configuration:

```ini
[Container]
# Image (REQUIRED)
Image=docker.io/library/nginx:latest

# Container name (optional, default: systemd-<filename>)
ContainerName=my-nginx

# Port publishing
PublishPort=8080:80
PublishPort=8443:443

# Volume mounts
Volume=/host/path:/container/path:Z

# Environment variables
Environment=KEY=value
Environment=DEBUG=true

# Resource limits
Memory=1G
CPUQuota=50%
```

#### [Service] Section (Recommended)

Systemd service options:

```ini
[Service]
Restart=always
TimeoutStartSec=300
```

#### [Install] Section (Required for Enable)

Defines when the service should start:

```ini
[Install]
# For rootful (system) services
WantedBy=multi-user.target

# For rootless (user) services
WantedBy=default.target

# Can specify both
WantedBy=multi-user.target default.target
```

### Minimal Example

The absolute minimum `.container` file:

```ini
[Container]
Image=docker.io/library/nginx:latest

[Install]
WantedBy=default.target
```

---

## Configuring Fetchit for Quadlet

### Basic Configuration

Create `config.yaml`:

```yaml
targets:
  - name: webapp-quadlet
    # Git repository containing Quadlet files
    url: https://github.com/myorg/containers.git
    branch: main
    # Directory in repo with Quadlet files
    targetPath: quadlet/
    # Check for updates every 5 minutes
    schedule: "*/5 * * * *"

    # Quadlet method configuration
    method:
      type: quadlet
      # Rootful deployment
      root: true
      # Enable and start services
      enable: true
      # Restart services on updates
      restart: false
```

### Rootless Configuration

For user-level deployments:

```yaml
targets:
  - name: dev-app
    url: https://github.com/myorg/dev-containers.git
    branch: develop
    targetPath: quadlet/
    schedule: "*/2 * * * *"

    method:
      type: quadlet
      # Rootless deployment
      root: false
      enable: true
      restart: true
```

### File Filtering with Glob Patterns

Filter specific file types:

```yaml
targets:
  - name: only-containers
    url: https://github.com/myorg/containers.git
    branch: main
    targetPath: quadlet/
    schedule: "*/5 * * * *"

    method:
      type: quadlet
      root: true
      enable: true
      # Only process .container files
      glob: "**/*.container"
```

Multiple file types:

```yaml
method:
  type: quadlet
  root: true
  enable: true
  # Process containers, volumes, and networks
  glob: "**/*.{container,volume,network}"
```

---

## Verifying Deployment

### Check Service Status

**Rootful**:
```bash
sudo systemctl status nginx.service
```

**Rootless**:
```bash
systemctl --user status nginx.service
```

### Check Container is Running

```bash
podman ps
# Look for container named "systemd-nginx"

podman logs systemd-nginx
```

### View Generated Service File

**Rootful**:
```bash
systemctl cat nginx.service
```

**Rootless**:
```bash
systemctl --user cat nginx.service
```

### Check Fetchit Logs

```bash
podman logs fetchit
```

### Debug Quadlet File Syntax

Use `systemd-analyze` to validate Quadlet files:

```bash
# For user services
systemd-analyze --user --generators=true verify nginx.service

# For system services
systemd-analyze --generators=true verify nginx.service
```

---

## Multi-Container Applications

### Example: Web Application with Database

Create three Quadlet files in your Git repository under `quadlet/`:

#### 1. `app-network.network` (Network)

```ini
[Network]
Subnet=172.20.0.0/16
Label=app=webapp

[Install]
WantedBy=multi-user.target
```

#### 2. `db-data.volume` (Volume)

```ini
[Volume]
Label=app=database

[Install]
WantedBy=multi-user.target
```

#### 3. `postgres.container` (Database)

```ini
[Unit]
Description=PostgreSQL database

[Container]
Image=docker.io/library/postgres:15
Network=app-network.network
Volume=db-data.volume:/var/lib/postgresql/data:Z
Environment=POSTGRES_PASSWORD=secret

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

#### 4. `webapp.container` (Application)

```ini
[Unit]
Description=Web application
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

### Service Start Order

Quadlet automatically creates dependencies:

1. `app-network-network.service` (network)
2. `db-data-volume.service` (volume)
3. `postgres.service` (database)
4. `webapp.service` (application - waits for postgres)

---

## Migrating from Systemd Method

### Step 1: Identify Current Deployments

List current systemd-based deployments in your fetchit config:

```yaml
# OLD (systemd method)
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

### Step 2: Convert Systemd Service Files to Quadlet

**Old systemd service file** (`httpd.service`):

```ini
[Unit]
Description=Apache web server

[Service]
ExecStartPre=/usr/bin/podman rm -f httpd
ExecStart=/usr/bin/podman run \\
    --name httpd \\
    -p 8080:80 \\
    -v /var/www:/usr/local/apache2/htdocs:Z \\
    docker.io/library/httpd:latest
ExecStop=/usr/bin/podman stop httpd
Restart=always

[Install]
WantedBy=multi-user.target
```

**New Quadlet file** (`httpd.container`):

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
- No `ExecStart` or `ExecStop` commands
- Container configuration in `[Container]` section
- Simpler, more declarative syntax
- No `--rm -f` logic needed (Quadlet handles cleanup)

### Step 3: Update Fetchit Configuration

```yaml
# NEW (quadlet method)
targets:
  - name: webapp-quadlet
    url: https://github.com/myorg/services.git
    branch: main
    # New directory in repo with Quadlet files
    targetPath: quadlet/
    schedule: "*/5 * * * *"
    method:
      type: quadlet
      root: true
      enable: true
      restart: false
```

### Step 4: Test the Migration

1. **Keep old systemd target** (temporarily)
2. **Add new quadlet target** with different name
3. **Deploy to test environment first**
4. **Verify functionality**
5. **Remove old systemd target** when confident

### Step 5: Update Repository Structure

```bash
# Old structure
services/
  systemd/
    httpd.service
    postgres.service

# New structure
services/
  systemd/         # Keep temporarily
    httpd.service
    postgres.service
  quadlet/         # New Quadlet files
    httpd.container
    postgres.container
    db-data.volume
```

### Migration Checklist

- [ ] Convert service files to Quadlet syntax
- [ ] Test Quadlet files locally
- [ ] Create new Git directory for Quadlet files
- [ ] Add new fetchit target with `type: quadlet`
- [ ] Test in staging/development environment
- [ ] Verify services start correctly
- [ ] Verify services restart on updates (if `restart: true`)
- [ ] Monitor logs for issues
- [ ] Remove old systemd target when stable

---

## Troubleshooting

### Service Not Generated

**Symptom**: After daemon-reload, service doesn't exist

```bash
systemctl list-units | grep myapp
# No results
```

**Solution**: Check Quadlet file syntax

```bash
# Validate syntax
systemd-analyze --user --generators=true verify myapp.service

# Check generator output
/usr/lib/systemd/system-generators/podman-system-generator --user --dryrun 2>&1 | grep -i error
```

**Common syntax errors**:
- Missing `[Container]` section
- Invalid key name (e.g., `jImage` instead of `Image`)
- Missing `Image=` directive

### XDG_RUNTIME_DIR Not Set (Rootless)

**Symptom**: Rootless deployments fail with permission errors

**Solution**:
```bash
export XDG_RUNTIME_DIR=/run/user/$(id -u)

# Make permanent (add to ~/.bashrc or ~/.zshrc)
echo 'export XDG_RUNTIME_DIR=/run/user/$(id -u)' >> ~/.bashrc
```

### Services Don't Persist After Logout (Rootless)

**Symptom**: Rootless services stop when you log out

**Solution**: Enable lingering

```bash
sudo loginctl enable-linger $USER

# Verify
loginctl show-user $USER | grep Linger
# Output: Linger=yes
```

### Permission Denied When Creating Directory

**Symptom**: Cannot create `/etc/containers/systemd/`

**Solution**: Use `sudo` for rootful deployments

```bash
sudo mkdir -p /etc/containers/systemd
sudo chmod 755 /etc/containers/systemd
```

### Image Pull Timeout

**Symptom**: Service fails to start with timeout error

**Solution**: Increase timeout in Quadlet file

```ini
[Service]
# Increase from default 90s to 5 minutes
TimeoutStartSec=300
```

### Container Name Conflict

**Symptom**: Error about container name already in use

**Solution**: Podman uses `systemd-<filename>` as default

```ini
[Container]
Image=myapp:latest
# Explicitly set unique name
ContainerName=my-unique-app
```

### Logs and Debugging

**View Fetchit logs**:
```bash
podman logs fetchit
podman logs -f fetchit  # Follow logs
```

**View service logs**:
```bash
# Rootful
sudo journalctl -u nginx.service -f

# Rootless
journalctl --user -u nginx.service -f
```

**View systemd daemon logs**:
```bash
sudo journalctl -u systemd --since "5 minutes ago"
```

---

## Next Steps

### Advanced Topics

- **Auto-updates**: Enable automatic container updates
  ```ini
  [Container]
  Image=myapp:latest
  AutoUpdate=registry
  ```

- **Health checks**: Monitor container health
  ```ini
  [Container]
  HealthCmd=/usr/bin/curl -f http://localhost/health || exit 1
  HealthInterval=30s
  HealthRetries=3
  ```

- **Resource limits**: Control CPU and memory
  ```ini
  [Container]
  Memory=1G
  CPUQuota=50%
  ```

- **Kubernetes YAML**: Deploy from Kubernetes manifests
  ```ini
  [Kube]
  Yaml=/path/to/deployment.yaml
  ```

### Documentation References

- [Quadlet File Format Reference](../../../QUADLET-REFERENCE.md)
- [Directory Structure Guide](../../001-podman-v4-upgrade/QUADLET-DIRECTORY-STRUCTURE.md)
- [systemd Integration Guide](../../001-podman-v4-upgrade/QUADLET-SYSTEMD-INTEGRATION-GUIDE.md)
- [Podman Quadlet Official Docs](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)

### Getting Help

- Check [GitHub Issues](https://github.com/containers/fetchit/issues)
- Review [Podman Quadlet Documentation](https://docs.podman.io/en/latest/markdown/podman-quadlet.1.html)
- Ask in Podman community forums

---

## Summary

Quadlet provides a simpler, more maintainable way to deploy containers with systemd integration:

✅ **Easy to learn** - Simpler syntax than systemd service files
✅ **Declarative** - Describe what you want, not how to do it
✅ **Integrated** - Native Podman and systemd integration
✅ **Git-based** - Fetchit monitors your repository and deploys automatically
✅ **Flexible** - Supports multiple file types and complex deployments

Start simple with a single `.container` file, then expand to volumes, networks, and multi-container applications as needed.
