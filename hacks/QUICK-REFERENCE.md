# Fetchit Local Testing - Quick Reference

## One-Liners

```bash
# Run all tests
sudo ./hacks/local-test.sh

# Run one test
sudo ./hacks/local-test.sh <test-name>

# List available tests
./hacks/local-test.sh --list

# Show help
./hacks/local-test.sh --help
```

## Common Test Commands

```bash
# Quick validation before PR
sudo ./hacks/local-test.sh

# Test raw container functionality
sudo ./hacks/local-test.sh raw-validate

# Test file transfer
sudo ./hacks/local-test.sh filetransfer-validate

# Test Kubernetes deployment
sudo ./hacks/local-test.sh kube-validate

# Test cleanup functionality
sudo ./hacks/local-test.sh clean-validate
```

## Build Commands

```bash
# Build all images
sudo ./hacks/local-test.sh --build-only

# Skip building (use existing images)
sudo ./hacks/local-test.sh --skip-build

# Clean and rebuild everything
sudo ./hacks/local-test.sh --clean --build-only
```

## Debugging

```bash
# View container logs
sudo podman logs fetchit

# Check running containers
sudo podman ps

# Check all containers (including stopped)
sudo podman ps -a

# Check pods
sudo podman pod ps

# Check volumes
sudo podman volume ls

# Check images
sudo podman images
```

## Cleanup Commands

```bash
# Remove all test containers
sudo podman rm -f fetchit cap1 cap2 colors1 colors2 httpd

# Remove all test pods
sudo podman pod rm -f colors_pod

# Remove all test volumes
sudo podman volume rm -f fetchit-volume test

# Remove test directories
sudo rm -rf /tmp/ft /tmp/disco /tmp/image

# Remove test systemd units
sudo rm -f /etc/systemd/system/httpd.service
sudo systemctl daemon-reload

# Nuclear option - remove everything
sudo podman system reset
```

## Test Names

- `raw-validate` - Basic container deployment
- `config-url-validate` - Config from URL
- `config-env-validate` - Config from env var
- `kube-validate` - Kubernetes YAML
- `filetransfer-validate` - File transfer
- `filetransfer-exact-file` - Single file transfer
- `systemd-validate` - Systemd units
- `clean-validate` - Cleanup/prune
- `glob-validate` - Glob patterns
- `imageload-validate` - Image loading

## Typical Workflow

```bash
# 1. Make your code changes
vim pkg/something.go

# 2. Build and test locally
sudo ./hacks/local-test.sh

# 3. If tests pass, commit and push
git add .
git commit -m "Your change"
git push

# 4. Create PR
gh pr create
```

## Exit Codes

- `0` - All tests passed
- `1` - One or more tests failed

## Environment Variables

The script uses these by default:
- `FETCHIT_IMAGE=quay.io/fetchit/fetchit:local-test`
- `COLORS_IMAGE=docker.io/mmumshad/simple-webapp-color:latest`
- `SYSTEMD_IMAGE=quay.io/fetchit/fetchit-systemd:local-test`
- `ANSIBLE_IMAGE=quay.io/fetchit/fetchit-ansible:local-test`
