# Quickstart: Podman v5 Migration Development

**Feature**: Podman v5 Dependency Upgrade
**Date**: 2025-12-30

## Overview

This guide helps developers set up their environment for working on the Podman v5 migration and provides common patterns for adapting fetchit code to Podman v5 APIs.

## Prerequisites

- Linux development environment (Ubuntu 22.04+, Fedora 38+, or similar)
- Git
- Make
- Docker or Podman (for testing)

## Development Environment Setup

### Step 1: Install Go 1.21+

**Note**: Exact Go version TBD based on Podman v5 requirements. Update after research phase.

```bash
# Download and install Go 1.21+ (example for 1.21.5)
wget https://go.dev/dl/go1.21.5.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.21.5.linux-amd64.tar.gz

# Add to PATH (add to ~/.bashrc or ~/.zshrc)
export PATH=$PATH:/usr/local/go/bin

# Verify installation
go version  # Should show go1.21.5 or later
```

### Step 2: Clone and Checkout Feature Branch

```bash
# Clone fetchit repository
git clone https://github.com/containers/fetchit.git
cd fetchit

# Checkout the Podman v5 upgrade branch
git checkout 001-podman-v4-upgrade

# Verify you're on the right branch
git branch --show-current  # Should show: 001-podman-v4-upgrade
```

### Step 3: Install Podman v5 (for local testing)

**Option A: Install from Package Manager (if available)**
```bash
# Fedora/RHEL
sudo dnf install podman

# Ubuntu (may need to add Podman PPA for v5)
sudo apt-get update
sudo apt-get install podman
```

**Option B: Build from Source**
```bash
# Install build dependencies
sudo apt-get install -y \
  btrfs-progs \
  git \
  golang-go \
  go-md2man \
  iptables \
  libassuan-dev \
  libbtrfs-dev \
  libc6-dev \
  libdevmapper-dev \
  libglib2.0-dev \
  libgpgme-dev \
  libgpg-error-dev \
  libprotobuf-dev \
  libprotobuf-c-dev \
  libseccomp-dev \
  libselinux1-dev \
  libsystemd-dev \
  pkg-config \
  uidmap

# Clone Podman v5
git clone https://github.com/containers/podman.git
cd podman
git checkout v5.0.0  # Use specific v5 tag from research

# Build and install
make
sudo make install

# Verify
podman --version  # Should show v5.x.x
```

### Step 4: Enable Podman Socket (for fetchit testing)

```bash
# Enable for current user
systemctl --user enable podman.socket --now

# OR enable for root
sudo systemctl enable podman.socket --now

# Verify socket is running
systemctl --user status podman.socket
# OR
sudo systemctl status podman.socket
```

### Step 5: Install Development Dependencies

```bash
cd fetchit

# Download Go module dependencies (will fail until go.mod is updated)
go mod download

# Vendor dependencies (fetchit uses vendoring)
go mod vendor

# Install additional build tools
sudo apt-get install -y libsystemd-dev libseccomp-dev pkg-config
```

## Building Fetchit with Podman v5

### Build Locally

```bash
# Clean previous builds
make clean

# Build fetchit binary
make build

# Verify build
./_output/bin/linux_amd64/fetchit --version
```

### Build Container Image

```bash
# Build fetchit container image
podman build . --file Dockerfile --tag quay.io/fetchit/fetchit-amd:latest
podman tag quay.io/fetchit/fetchit-amd:latest quay.io/fetchit/fetchit:latest
```

## Running Tests

### Unit Tests (after adding new tests)

```bash
# Run all unit tests
go test ./... -v

# Run tests for specific package
go test ./pkg/engine -v

# Run tests with coverage
go test ./... -cover -coverprofile=coverage.out

# View coverage report
go tool cover -html=coverage.out
```

### Integration Tests (GitHub Actions locally)

```bash
# Use act to run GitHub Actions locally (optional)
# Install act: https://github.com/nektos/act
act -j build
act -j raw-validate
```

### Manual Functional Testing

```bash
# Start fetchit with example configuration
podman run -d --rm --name fetchit \
    -v fetchit-volume:/opt \
    -v ./examples/readme-config.yaml:/opt/config.yaml \
    -v /run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock \
    --security-opt label=disable \
    quay.io/fetchit/fetchit:latest

# Watch logs
podman logs -f fetchit

# Verify containers deployed
podman ps

# Clean up
podman stop fetchit
podman rm -f colors1 colors2  # Example containers from readme-config
podman volume rm fetchit-volume
```

## Common Podman v5 Migration Patterns

### Pattern 1: Updating Container Spec Generation

**Before (v4)**:
```go
s := specgen.NewSpecGenerator(fetchitImage, false)
s.Name = "container-name"
s.Privileged = true
```

**After (v5)**: TBD based on research
```go
// Will be updated after identifying v5 API changes
s := specgen.NewSpecGenerator(fetchitImage, false)
// Update any changed field names or types
```

### Pattern 2: Container Operations

**Before (v4)**:
```go
createResponse, err := containers.CreateWithSpec(conn, s, nil)
if err != nil {
    return createResponse, err
}
```

**After (v5)**: TBD based on research
```go
// Check for signature changes in v5
// Update error handling if needed
```

### Pattern 3: Client Initialization

**Before (v4)**:
```go
conn context.Context
// Connection established via Podman socket
```

**After (v5)**: TBD based on research
```go
// Update if v5 changes connection initialization
```

## Development Workflow

### Step-by-Step Implementation Approach

1. **Update Dependencies First**
   ```bash
   # Edit go.mod to target v5
   # Update Go version line
   # Update podman import to v5
   go mod tidy
   go mod vendor
   ```

2. **Fix Compilation Errors**
   ```bash
   # Attempt build to see all breaking changes
   make build 2>&1 | tee build-errors.txt

   # Fix errors file by file
   # Start with pkg/engine/container.go (most critical)
   ```

3. **Update Tests**
   ```bash
   # Run tests to find runtime issues
   go test ./... -v 2>&1 | tee test-errors.txt

   # Fix test failures
   ```

4. **Validate Integration Tests**
   ```bash
   # Push to feature branch
   git add .
   git commit -m "WIP: Update to Podman v5"
   git push origin 001-podman-v4-upgrade

   # Check GitHub Actions results
   ```

### Incremental PR Strategy

Create small, focused PRs:
1. **PR 1**: Update go.mod and go version only
2. **PR 2**: Fix container.go and raw.go (core container operations)
3. **PR 3**: Fix image.go and kube.go
4. **PR 4**: Fix remaining pkg/engine/ files
5. **PR 5**: Update GitHub Actions workflows
6. **PR 6**: Add new unit tests

## Troubleshooting

### Issue: Build fails with "missing go.sum entry"

**Solution**:
```bash
go mod tidy
go mod vendor
```

### Issue: "cannot find package github.com/containers/podman/v5"

**Solution**: Ensure go.mod has correct v5 import path:
```go
require (
    github.com/containers/podman/v5 v5.x.x
    // ... other deps
)
```

### Issue: Tests fail with "connection refused" to Podman socket

**Solution**: Ensure Podman socket is running:
```bash
systemctl --user status podman.socket
# If not running:
systemctl --user start podman.socket
```

### Issue: GitHub Actions tests fail but local tests pass

**Solution**: Check GitHub Actions logs for environment differences:
- Go version mismatch
- Podman version mismatch
- Missing build dependencies

## Resources

- **Podman v5 Documentation**: https://docs.podman.io/
- **Podman v5 API Reference**: https://pkg.go.dev/github.com/containers/podman/v5
- **Fetchit Documentation**: https://fetchit.readthedocs.io/
- **Feature Spec**: [specs/001-podman-v4-upgrade/spec.md](spec.md)
- **Implementation Plan**: [specs/001-podman-v4-upgrade/plan.md](plan.md)
- **Research Notes**: [specs/001-podman-v4-upgrade/research.md](research.md)

## Getting Help

- **Podman Community**: #podman on libera.chat IRC
- **Fetchit Issues**: https://github.com/containers/fetchit/issues
- **Feature Branch Discussions**: Create issue linking to PR for questions

## Next Steps After Setup

1. Review [research.md](research.md) for identified breaking changes
2. Review [plan.md](plan.md) for implementation strategy
3. Run `/speckit.tasks` to generate detailed task list
4. Begin implementation following task order
