# Quadlet Examples

This directory contains example Quadlet files for deploying containers with fetchit using Podman Quadlet.

## What is Quadlet?

Quadlet is a systemd generator integrated into Podman (v4.4+) that converts declarative container configuration files (`.container`, `.volume`, `.network`, `.kube`) into systemd service units. This eliminates the need for writing complex systemd service files and provides a Podman-native way to manage containers via systemd.

## Example Files

### Basic Container Example

- **simple.container** - Minimal nginx container deployment
  - Demonstrates basic container configuration
  - Publishes port 8080 to host port 80
  - Automatically starts on boot

### Web Server with Volume and Network

- **httpd.container** - Apache web server with volume and network
  - Mounts a named volume for persistent content
  - Connects to a custom network
  - Demonstrates multi-resource dependencies

- **httpd.volume** - Named volume for httpd content
  - Persistent storage for web content
  - Automatically created before the container starts

- **httpd.network** - Custom network for httpd
  - Isolated network for web services
  - Created before containers using it

### Kubernetes Pod Example

- **colors.kube** - Multi-container pod from Kubernetes YAML
  - Deploys a pod with multiple containers
  - Demonstrates Kubernetes manifest support in Quadlet
  - Useful for migrating from Kubernetes to Podman

## Usage with Fetchit

See the configuration examples in the parent directory:
- `quadlet-config.yaml` - Rootful deployment example
- `quadlet-rootless.yaml` - Rootless deployment example

## Testing Quadlet Files Locally

### Rootful (System-wide)

```bash
# Copy files to systemd directory
sudo cp *.container *.volume *.network /etc/containers/systemd/

# Reload systemd
sudo systemctl daemon-reload

# Check generated services
systemctl list-units | grep -E '(simple|httpd)'

# Start a service
sudo systemctl start simple.service

# Check status
sudo systemctl status simple.service

# View container
podman ps
```

### Rootless (User-level)

```bash
# Enable lingering (allows services to run when not logged in)
sudo loginctl enable-linger $USER

# Create directory
mkdir -p ~/.config/containers/systemd/

# Copy files
cp *.container *.volume *.network ~/.config/containers/systemd/

# Reload systemd
systemctl --user daemon-reload

# Start service
systemctl --user start simple.service

# Check status
systemctl --user status simple.service

# View container
podman ps
```

## Service Naming Conventions

Quadlet automatically generates systemd service names:
- `myapp.container` → `myapp.service` (container named `systemd-myapp`)
- `data.volume` → `data-volume.service`
- `app-net.network` → `app-net-network.service`
- `webapp.kube` → `webapp.service`

## Further Reading

- [Podman Quadlet Documentation](https://docs.podman.io/en/latest/markdown/podman-systemd.unit.5.html)
- [Fetchit Quadlet Quickstart](../../specs/002-quadlet-support/quickstart.md)
- [Quadlet Migration Guide](../../docs/quadlet-migration.md)
