# Fetchit Local Testing

This directory contains scripts for testing fetchit functionality locally before submitting PRs.

## Quick Start

```bash
# Run all tests
cd hacks
sudo ./local-test.sh

# Run a specific test
sudo ./local-test.sh raw-validate

# List available tests
./local-test.sh --list

# Build images only (no testing)
sudo ./local-test.sh --build-only

# Clean up and run tests
sudo ./local-test.sh --clean
```

## Prerequisites

The script requires:
- `podman` - Container runtime
- `jq` - JSON processor for parsing container data
- `docker` (optional) - Used for some builds, can use podman if unavailable
- Root/sudo access - Required for podman operations

On Fedora:
```bash
sudo dnf install -y podman jq
```

## Available Tests

The script mirrors the GitHub Actions validation tests:

| Test Name | Description |
|-----------|-------------|
| `raw-validate` | Tests raw container deployment with capabilities and labels |
| `config-url-validate` | Tests loading config from remote URL |
| `config-env-validate` | Tests config via environment variable |
| `kube-validate` | Tests Kubernetes YAML pod deployment |
| `filetransfer-validate` | Tests file transfer from Git to local filesystem |
| `filetransfer-exact-file` | Tests single file transfer |
| `systemd-validate` | Tests systemd unit file deployment |
| `clean-validate` | Tests cleanup/prune functionality |
| `glob-validate` | Tests glob pattern matching in file paths |
| `imageload-validate` | Tests loading container images from HTTP |

## Usage Examples

### Run All Tests
```bash
sudo ./local-test.sh
```

### Run Specific Test
```bash
sudo ./local-test.sh raw-validate
```

### Build Images First, Then Test
```bash
# Build all images
sudo ./local-test.sh --build-only

# Run tests without rebuilding
sudo ./local-test.sh --skip-build
```

### Clean Environment and Test
```bash
sudo ./local-test.sh --clean
```

## Test Output

The script provides colored output:
- 🔵 **Blue** - Informational messages
- 🟢 **Green** - Success messages
- 🟡 **Yellow** - Warnings
- 🔴 **Red** - Errors

Example output:
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  TEST: Raw Container Deployment
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ℹ Waiting for container: colors
✓ PASSED: raw-validate
```

## Test Summary

After all tests complete, you'll see a summary:
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  TEST SUMMARY
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Tests run:    10
Tests passed: 10
Tests failed: 0

All tests passed!
```

## Cleanup

The script automatically cleans up between tests. To manually clean up:
```bash
# Remove test containers
sudo podman rm -f fetchit cap1 cap2 colors1 colors2 httpd

# Remove test pods
sudo podman pod rm -f colors_pod

# Remove test volumes
sudo podman volume rm -f fetchit-volume test

# Remove test files
sudo rm -rf /tmp/ft /tmp/disco /tmp/image
sudo rm -f /etc/systemd/system/httpd.service
```

## Troubleshooting

### Test fails with "operation not permitted"
Make sure you're running with `sudo`:
```bash
sudo ./local-test.sh
```

### Container logs needed
The script automatically shows the last 50 lines of logs when a test fails. To manually view logs:
```bash
sudo podman logs fetchit
```

### Port already in use
If port 8080 is in use for the imageload test:
```bash
# Stop any services using port 8080
sudo netstat -tulpn | grep 8080
# Or clean up all test containers
sudo podman rm -f $(sudo podman ps -aq)
```

### Systemd tests skipped
The systemd and ansible tests require building special images. These are optional and will be skipped if not available:
```bash
# Build systemd image
make build-systemd-cross-build-linux-amd64

# Build ansible image
make build-ansible-cross-build-linux-amd64
```

## Integration with CI/CD

This script is designed to mirror the GitHub Actions tests, so:
1. Run locally before pushing: `sudo ./local-test.sh`
2. Fix any failures
3. Push to PR
4. GitHub Actions should pass

## Adding New Tests

To add a new test:

1. Create a test function following the pattern:
```bash
test_my_new_test() {
    print_header "TEST: My New Feature"
    cleanup

    # Your test logic here

    if [ test_passed ]; then
        record_test "my-new-test" "pass"
    else
        record_test "my-new-test" "fail"
        return 1
    fi
}
```

2. Add to `run_all_tests()` function
3. Add to the case statement in the main script
4. Update the help text and this README

## Notes

- Tests run sequentially with cleanup between each test
- Some tests require external network access (config-url-validate, imageload-validate)
- Tests create temporary files in `/tmp/ft`, `/tmp/disco`, `/tmp/image`
- Tests may create systemd units in `/etc/systemd/system/`
- The script preserves exit codes: 0 for success, 1 for failure
