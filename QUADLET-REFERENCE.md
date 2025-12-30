# Podman Quadlet File Format Reference

Comprehensive reference for Podman Quadlet file formats including `.container`, `.volume`, `.network`, and `.kube` files.

## Table of Contents

1. [Overview](#overview)
2. [File Locations](#file-locations)
3. [Service Naming Conventions](#service-naming-conventions)
4. [Container Files (.container)](#container-files-container)
5. [Volume Files (.volume)](#volume-files-volume)
6. [Network Files (.network)](#network-files-network)
7. [Kube Files (.kube)](#kube-files-kube)
8. [Systemd Dependencies](#systemd-dependencies)
9. [Common Patterns](#common-patterns)

---

## Overview

Podman Quadlets allow users to manage containers, pods, volumes, networks, and images declaratively via systemd unit files. This streamlines container management on Linux systems without the complexity of full orchestration tools like Kubernetes.

Quadlet files are processed during systemd startup or when you run `systemctl daemon-reload`. The quadlet generator converts these simplified configuration files into standard systemd service units.

### Supported File Types

- `.container` - Container units (equivalent to `podman run`)
- `.volume` - Volume units (creates named Podman volumes)
- `.network` - Network units (creates Podman networks)
- `.kube` - Kubernetes YAML deployments
- `.pod` - Pod units (manages Podman pods)
- `.image` - Image units (pulls container images)
- `.build` - Build units (builds container images)

---

## File Locations

### System-wide (Root) Containers

```
/etc/containers/systemd/
/usr/share/containers/systemd/
```

### User Containers (Rootless)

```
~/.config/containers/systemd/
```

Quadlet supports subdirectories within these locations for better organization.

### Example Directory Structure

```
~/.config/containers/systemd/
├── web/
│   ├── nginx.container
│   └── web-data.volume
├── database/
│   ├── postgres.container
│   └── db-data.volume
└── shared/
    └── app-network.network
```

---

## Service Naming Conventions

Understanding how Quadlet filenames map to systemd service names is crucial for managing dependencies.

### Basic Naming Rules

| Quadlet File | Systemd Service | Podman Resource Name |
|--------------|----------------|---------------------|
| `nginx.container` | `nginx.service` | `systemd-nginx` |
| `web-data.volume` | `web-data-volume.service` | `systemd-web-data` |
| `app-net.network` | `app-net-network.service` | `systemd-app-net` |
| `webapp.pod` | `webapp-pod.service` | `systemd-webapp` |

### Key Points

- Container files create a `.service` unit with the same base name
- Volume files create a `-volume.service` unit
- Network files create a `-network.service` unit
- Pod files create a `-pod.service` unit
- Podman resources are prefixed with `systemd-` by default

### Custom Naming

You can override default names using specific directives:

```ini
[Container]
ContainerName=my-custom-name  # Instead of systemd-filename

[Volume]
VolumeName=my-custom-volume  # Instead of systemd-filename

[Network]
NetworkName=my-custom-network  # Instead of systemd-filename
```

---

## Container Files (.container)

Container files define a container to run as a systemd service. They support extensive configuration options similar to `podman run`.

### Minimal Example

```ini
[Unit]
Description=Nginx web server

[Container]
Image=docker.io/library/nginx:latest

[Service]
Restart=always

[Install]
WantedBy=multi-user.target default.target
```

### Complete Syntax Reference

#### [Unit] Section

Standard systemd unit options:

```ini
[Unit]
Description=Human-readable description
Documentation=https://example.com/docs
After=network-online.target
Wants=network-online.target
```

#### [Container] Section

Required fields:
- `Image=` - Container image to use (REQUIRED)

Common options:

```ini
[Container]
# Image configuration
Image=docker.io/library/nginx:latest
# Always use fully qualified names for performance and robustness

# Container naming
ContainerName=my-nginx
# Default: systemd-<filename>

# Command and arguments
Exec=sleep 60
# Additional arguments passed to the container command

# Networking
Network=host
# Options: host, bridge, none, container:<name>, <network-name>.network
# If ends with .network, creates dependency on that network unit

PublishPort=8080:80
PublishPort=8443:443
# Format: [HOST_IP:][HOST_PORT:]CONTAINER_PORT[/PROTOCOL]
# Can be listed multiple times

IP=10.88.64.128
IP6=fd46:db93:aa76:ac37::10
# Static IP addresses

# Volume mounting
Volume=/host/path:/container/path:Z
Volume=my-data.volume:/var/lib/data:Z
# If SOURCE ends with .volume, creates dependency on that volume unit
# Options: :z (shared SELinux label), :Z (private SELinux label), :ro (read-only)

# Environment variables
Environment=KEY=value
Environment=DEBUG=true
EnvironmentFile=/path/to/env/file
# Can be listed multiple times

# Resource limits
Memory=1G
MemorySwap=2G
CPUQuota=50%

# Security
User=1000
Group=1000
SecurityLabelDisable=false
SecurityLabelType=container_runtime_t
SecurityLabelLevel=s0:c1,c2

# Labels and annotations
Label=version=1.0
Label=environment=production
Annotation=key=value

# Additional options
AddDevice=/dev/fuse
AddDevice=/dev/net/tun
AutoUpdate=registry
# Options: registry, local, disabled
HealthCmd=/usr/bin/healthcheck.sh
HealthInterval=30s
HealthRetries=3
HealthStartPeriod=60s
HealthTimeout=10s
LogDriver=journald
Notify=true
PodmanArgs=--log-level=debug
# Raw arguments passed to podman run
Pull=missing
# Options: always, missing, never, newer
ReadOnly=false
Tmpfs=/tmp
Timezone=local
WorkingDir=/app
```

#### [Service] Section

Standard systemd service options:

```ini
[Service]
Restart=always
# Options: no, on-success, on-failure, on-abnormal, on-watchdog, on-abort, always
TimeoutStartSec=300
Type=notify
# Quadlet sets this automatically for containers with Notify=true
```

#### [Install] Section

```ini
[Install]
WantedBy=multi-user.target default.target
# multi-user.target for system services
# default.target for user services
```

### Practical Examples

#### Simple Web Server

```ini
[Unit]
Description=Nginx web server
After=network-online.target
Wants=network-online.target

[Container]
Image=docker.io/library/nginx:latest
ContainerName=nginx
PublishPort=8080:80
Volume=nginx-html.volume:/usr/share/nginx/html:Z,ro

[Service]
Restart=always
TimeoutStartSec=60

[Install]
WantedBy=multi-user.target default.target
```

#### Application with Database

```ini
[Unit]
Description=Web application
After=network-online.target postgres.service
Requires=postgres.service

[Container]
Image=docker.io/myapp:latest
ContainerName=webapp
Network=app-network.network
PublishPort=3000:3000
Environment=DATABASE_URL=postgresql://postgres:5432/myapp
Environment=NODE_ENV=production
Volume=app-data.volume:/app/data:Z

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

#### Rootless Container with Custom User

```ini
[Unit]
Description=Application running as specific user

[Container]
Image=docker.io/myapp:latest
User=1000
Group=1000
Volume=/home/user/app:/app:Z
Environment=HOME=/app
WorkingDir=/app
Exec=python server.py

[Service]
Restart=on-failure

[Install]
WantedBy=default.target
```

---

## Volume Files (.volume)

Volume files create named Podman volumes that can be referenced by container units.

### Minimal Example

```ini
[Volume]
```

This creates a volume with the default name `systemd-<filename>`.

### Complete Syntax Reference

```ini
[Unit]
Description=Data volume for application

[Volume]
# Volume naming
VolumeName=my-data
# Default: systemd-<filename>

# Volume driver
Driver=local
# Default: local

# Volume labels
Label=app=webapp
Label=environment=production

# Image-based volume
Image=docker.io/library/alpine:latest
# Base image for volume initialization

# Copy image content to volume
Copy=true
# When using Image=, copy image content to volume

# Driver options
Device=/dev/sdb1
Type=ext4
Options=uid=1000,gid=1000

# Podman-specific arguments
PodmanArgs=--opt=o=nodev

[Install]
WantedBy=multi-user.target default.target
```

### Practical Examples

#### Simple Named Volume

```ini
[Unit]
Description=PostgreSQL data volume

[Volume]
VolumeName=postgres-data
Label=app=database
Label=backup=daily

[Install]
WantedBy=multi-user.target
```

#### Volume with Initialization

```ini
[Unit]
Description=Nginx content volume

[Volume]
VolumeName=nginx-html
Image=docker.io/library/nginx:latest
Copy=true
Label=app=webserver

[Install]
WantedBy=multi-user.target
```

#### Volume with Custom Driver Options

```ini
[Unit]
Description=Application data with custom options

[Volume]
VolumeName=app-data
Driver=local
Device=/mnt/storage
Type=ext4
Options=uid=1000,gid=1000

[Install]
WantedBy=multi-user.target
```

---

## Network Files (.network)

Network files create Podman networks that can be used by containers and pods.

### Minimal Example

```ini
[Network]
```

This creates a bridge network with the default name `systemd-<filename>`.

### Complete Syntax Reference

```ini
[Unit]
Description=Application network

[Network]
# Network naming
NetworkName=my-network
# Default: systemd-<filename>

# Network driver
Driver=bridge
# Options: bridge, macvlan, ipvlan, host
# Default: bridge

# Subnet configuration
Subnet=10.89.0.0/24
Gateway=10.89.0.1
IPRange=10.89.0.0/28

# IPv6 support
IPv6=true
Subnet=fd12:3456:789a::/64
Gateway=fd12:3456:789a::1

# Network options
Internal=false
# true = no external access

DisableDNS=false
# true = disable DNS resolution

# DNS configuration
DNS=8.8.8.8
DNS=8.8.4.4

# Network labels
Label=app=webapp
Label=environment=production

# Driver options
Options=com.docker.network.bridge.name=br-custom
Options=com.docker.network.driver.mtu=1450

# Podman-specific arguments
PodmanArgs=--opt=mtu=1450

[Install]
WantedBy=multi-user.target default.target
```

### Practical Examples

#### Simple Bridge Network

```ini
[Unit]
Description=Application bridge network

[Network]
NetworkName=app-network
Subnet=172.20.0.0/16
Gateway=172.20.0.1
Label=app=myapp

[Install]
WantedBy=multi-user.target
```

#### Internal Network (No External Access)

```ini
[Unit]
Description=Database internal network

[Network]
NetworkName=db-internal
Subnet=10.100.0.0/24
Internal=true
Label=tier=database
Label=access=internal

[Install]
WantedBy=multi-user.target
```

#### IPv6 Enabled Network

```ini
[Unit]
Description=Dual-stack network

[Network]
NetworkName=dual-stack
Subnet=172.30.0.0/16
Gateway=172.30.0.1
IPv6=true
Subnet=fd00:dead:beef::/48
Gateway=fd00:dead:beef::1
DNS=8.8.8.8
DNS=2001:4860:4860::8888

[Install]
WantedBy=multi-user.target
```

---

## Kube Files (.kube)

Kube files allow you to manage containers from Kubernetes YAML files using systemd.

### Minimal Example

```ini
[Unit]
Description=Kubernetes deployment

[Kube]
Yaml=/path/to/deployment.yaml

[Install]
WantedBy=multi-user.target
```

### Complete Syntax Reference

```ini
[Unit]
Description=Kubernetes deployment

[Kube]
# Kubernetes YAML file
Yaml=/path/to/kubernetes.yaml
# Path (absolute or relative to unit file location) to Kubernetes YAML
# REQUIRED - can be listed multiple times

# Auto-update
AutoUpdate=registry
# Options: registry, local, disabled

# Networking
Network=host
# Options: host, bridge, none, <network-name>.network

# Port publishing
PublishPort=8080:80
PublishPort=8443:443

# Configuration maps
ConfigMap=/path/to/configmap.yaml

# Volume mounting
Volume=/host/path:/container/path:Z

# Podman-specific arguments
PodmanArgs=--log-level=debug

# Update policy
ExitCodePropagation=all
# Options: all, any, none

[Service]
Restart=always

[Install]
WantedBy=multi-user.target default.target
```

### Practical Examples

#### Simple Kubernetes Deployment

```ini
[Unit]
Description=Web application from Kubernetes YAML
After=network-online.target
Wants=network-online.target

[Kube]
Yaml=/etc/containers/k8s/webapp-deployment.yaml
Network=app-network.network
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

#### Multi-file Kubernetes Setup

```ini
[Unit]
Description=Complete application stack

[Kube]
Yaml=/etc/containers/k8s/deployment.yaml
Yaml=/etc/containers/k8s/service.yaml
Yaml=/etc/containers/k8s/configmap.yaml
ConfigMap=/etc/containers/k8s/app-config.yaml
Volume=app-data.volume:/data:Z
AutoUpdate=registry

[Service]
Restart=always
TimeoutStartSec=300

[Install]
WantedBy=multi-user.target
```

---

## Systemd Dependencies

Understanding how Quadlet creates dependencies between units is crucial for building reliable multi-container applications.

### Automatic Dependency Creation

Quadlet automatically creates systemd dependencies when you reference other Quadlet units:

| Reference Type | Automatic Dependency |
|---------------|---------------------|
| `Volume=data.volume:/path` | `Requires=data-volume.service` + `After=data-volume.service` |
| `Network=app.network` | `Requires=app-network.service` + `After=app-network.service` |
| Referenced `.volume` file | Creates volume service dependency |
| Referenced `.network` file | Creates network service dependency |

### Dependency Detection Rules

1. **Volume References**: If a volume source ends with `.volume`, Quadlet looks for the corresponding Quadlet file and creates a dependency.
2. **Network References**: If a network name ends with `.network`, Quadlet creates a dependency on that network service.
3. **Naming**: The Podman resource uses the `VolumeName=` or `NetworkName=` if set, otherwise `systemd-<filename>`.

### Manual Dependencies

You can also create manual dependencies using standard systemd directives:

```ini
[Unit]
Description=Web application
After=database.service
Requires=database.service
Wants=cache.service
```

### Dependency Types

- `Requires=` - Hard dependency (if dependency fails, this unit fails)
- `Wants=` - Soft dependency (if dependency fails, this unit continues)
- `After=` - Order dependency (start after this unit)
- `Before=` - Order dependency (start before this unit)
- `BindsTo=` - Strong binding (stop if dependency stops)

### Practical Dependency Examples

#### Web App with Database and Volume

**postgres.container:**
```ini
[Unit]
Description=PostgreSQL database

[Container]
Image=docker.io/library/postgres:15
ContainerName=postgres
Network=db-network.network
Volume=postgres-data.volume:/var/lib/postgresql/data:Z
Environment=POSTGRES_PASSWORD=secret

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**webapp.container:**
```ini
[Unit]
Description=Web application
After=postgres.service
Requires=postgres.service

[Container]
Image=docker.io/myapp:latest
ContainerName=webapp
Network=db-network.network
PublishPort=3000:3000
Environment=DATABASE_URL=postgresql://postgres:5432/myapp

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**postgres-data.volume:**
```ini
[Unit]
Description=PostgreSQL data volume

[Volume]
VolumeName=postgres-data
Label=app=database

[Install]
WantedBy=multi-user.target
```

**db-network.network:**
```ini
[Unit]
Description=Database network

[Network]
NetworkName=db-network
Subnet=172.25.0.0/16
Internal=true

[Install]
WantedBy=multi-user.target
```

#### Dependency Chain Example

```
db-network.network (network service)
     |
     v
postgres-data.volume (volume service)
     |
     v
postgres.container (database service)
     |
     v
webapp.container (application service)
```

Systemd ensures they start in order:
1. `db-network-network.service`
2. `postgres-data-volume.service`
3. `postgres.service`
4. `webapp.service`

---

## Common Patterns

### Pattern 1: Simple Web Server

Files:
- `nginx.container`
- `nginx-html.volume`

**nginx-html.volume:**
```ini
[Volume]
VolumeName=nginx-html
Label=app=nginx

[Install]
WantedBy=multi-user.target
```

**nginx.container:**
```ini
[Unit]
Description=Nginx web server

[Container]
Image=docker.io/library/nginx:latest
PublishPort=8080:80
Volume=nginx-html.volume:/usr/share/nginx/html:Z

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

### Pattern 2: Three-Tier Application

Files:
- `app-network.network`
- `db-data.volume`
- `postgres.container`
- `redis.container`
- `webapp.container`

**app-network.network:**
```ini
[Network]
Subnet=172.30.0.0/24

[Install]
WantedBy=multi-user.target
```

**db-data.volume:**
```ini
[Volume]

[Install]
WantedBy=multi-user.target
```

**postgres.container:**
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

**redis.container:**
```ini
[Unit]
Description=Redis cache

[Container]
Image=docker.io/library/redis:7
Network=app-network.network

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**webapp.container:**
```ini
[Unit]
Description=Web application
After=postgres.service redis.service
Requires=postgres.service
Wants=redis.service

[Container]
Image=docker.io/myapp:latest
Network=app-network.network
PublishPort=8080:3000
Environment=DATABASE_URL=postgresql://systemd-postgres:5432/myapp
Environment=REDIS_URL=redis://systemd-redis:6379

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

### Pattern 3: Rootless User Container

Setup:
```bash
mkdir -p ~/.config/containers/systemd
```

**~/.config/containers/systemd/myapp.container:**
```ini
[Unit]
Description=My personal application

[Container]
Image=docker.io/myapp:latest
PublishPort=8080:8080
Volume=%h/app-data:/data:Z
# %h expands to user's home directory

[Service]
Restart=always

[Install]
WantedBy=default.target
```

Manage with:
```bash
systemctl --user daemon-reload
systemctl --user start myapp.service
systemctl --user enable myapp.service
```

### Pattern 4: Container with Health Checks

```ini
[Unit]
Description=Application with health monitoring

[Container]
Image=docker.io/myapp:latest
HealthCmd=/usr/bin/curl -f http://localhost:8080/health || exit 1
HealthInterval=30s
HealthRetries=3
HealthStartPeriod=60s
HealthTimeout=10s
PublishPort=8080:8080

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

### Pattern 5: Auto-updating Container

```ini
[Unit]
Description=Auto-updating container

[Container]
Image=docker.io/library/nginx:latest
AutoUpdate=registry
# Checks for updates when podman-auto-update runs
Label=io.containers.autoupdate=registry
PublishPort=8080:80

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

Enable auto-update timer:
```bash
systemctl enable --now podman-auto-update.timer
```

### Pattern 6: Multi-Container Application with Shared Network

**shared-network.network:**
```ini
[Network]
Subnet=10.89.0.0/24
DNS=8.8.8.8

[Install]
WantedBy=multi-user.target
```

**frontend.container:**
```ini
[Unit]
Description=Frontend service

[Container]
Image=docker.io/frontend:latest
Network=shared-network.network
PublishPort=8080:80
Environment=BACKEND_URL=http://systemd-backend:3000

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**backend.container:**
```ini
[Unit]
Description=Backend service

[Container]
Image=docker.io/backend:latest
Network=shared-network.network
Environment=DB_HOST=systemd-database

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

**database.container:**
```ini
[Unit]
Description=Database service

[Container]
Image=docker.io/library/postgres:15
Network=shared-network.network
Volume=db-data.volume:/var/lib/postgresql/data:Z
Environment=POSTGRES_PASSWORD=secret

[Service]
Restart=always

[Install]
WantedBy=multi-user.target
```

---

## Managing Quadlet Services

### Installation

1. Create Quadlet files in the appropriate directory
2. Reload systemd: `systemctl daemon-reload` (or `systemctl --user daemon-reload`)
3. Enable and start services:

```bash
# System-wide
systemctl enable --now nginx.service

# User services
systemctl --user enable --now myapp.service
```

### Verification

```bash
# Check service status
systemctl status nginx.service

# View logs
journalctl -u nginx.service -f

# List generated services
systemctl list-units "*.service" | grep systemd-

# Check container status
podman ps
```

### Troubleshooting

```bash
# Check quadlet file syntax
podman quadlet --dryrun /path/to/file.container

# View generated service file
systemctl cat nginx.service

# Debug systemd issues
systemctl status nginx.service
journalctl -xe -u nginx.service
```

---

## Best Practices

1. **Use Fully Qualified Image Names**
   - Always use `docker.io/library/nginx:latest` instead of `nginx`
   - Improves performance and robustness

2. **Set Explicit Dependencies**
   - Use `After=` and `Requires=` for container dependencies
   - Let Quadlet handle volume and network dependencies automatically

3. **Use Named Volumes**
   - Create `.volume` files for persistent data
   - Easier to manage than bind mounts

4. **Configure Restart Policies**
   - Use `Restart=always` for production services
   - Use `Restart=on-failure` for development

5. **Label Your Resources**
   - Add meaningful labels for organization
   - Helps with automation and monitoring

6. **Security**
   - Use SELinux labels (`:Z` or `:z`) for volumes
   - Run rootless containers when possible
   - Set appropriate user/group IDs

7. **Networking**
   - Use custom networks for isolation
   - Use `Internal=true` for backend networks
   - Document port mappings clearly

8. **Documentation**
   - Use descriptive `Description=` fields
   - Add comments explaining complex configurations
   - Document dependencies in comments

---

## Additional Resources

- [Podman systemd.unit documentation](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Podman Quadlet documentation](https://docs.podman.io/en/latest/markdown/podman-quadlet.1.html)
- [Red Hat: Multi-container application with Quadlet](https://www.redhat.com/en/blog/multi-container-application-podman-quadlet)
- [Red Hat: Make systemd better for Podman with Quadlet](https://www.redhat.com/en/blog/quadlet-podman)
- [Oracle: Podman Quadlets](https://docs.oracle.com/en/operating-systems/oracle-linux/podman/quadlets.html)
- [Podman Desktop: Podman Quadlets blog](https://podman-desktop.io/blog/podman-quadlet)
- [LinuxConfig: How to run Podman containers under Systemd with Quadlet](https://linuxconfig.org/how-to-run-podman-containers-under-systemd-with-quadlet)

---

## Quick Reference Card

### File Locations

| Type | Root | User |
|------|------|------|
| System | `/etc/containers/systemd/` | N/A |
| User | N/A | `~/.config/containers/systemd/` |

### Service Names

| File | Service | Resource |
|------|---------|----------|
| `app.container` | `app.service` | `systemd-app` |
| `data.volume` | `data-volume.service` | `systemd-data` |
| `net.network` | `net-network.service` | `systemd-net` |

### Common Commands

```bash
# Reload after changes
systemctl daemon-reload

# Start service
systemctl start app.service

# Enable on boot
systemctl enable app.service

# Check status
systemctl status app.service

# View logs
journalctl -u app.service -f

# List Quadlet services
systemctl list-units "*systemd-*"
```

### Minimal Templates

**.container:**
```ini
[Container]
Image=docker.io/library/image:tag
[Install]
WantedBy=multi-user.target
```

**.volume:**
```ini
[Volume]
[Install]
WantedBy=multi-user.target
```

**.network:**
```ini
[Network]
[Install]
WantedBy=multi-user.target
```

**.kube:**
```ini
[Kube]
Yaml=/path/to/file.yaml
[Install]
WantedBy=multi-user.target
```

---

*Generated: 2025-12-30*
