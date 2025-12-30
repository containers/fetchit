# Functional Testing Guide: Podman v5 Upgrade

**Feature**: Podman v5 Dependency Upgrade
**Date**: 2025-12-30
**Purpose**: Manual functional validation of all fetchit mechanisms with Podman v5.7.0

## Prerequisites

Before running functional tests:

1. **Podman v5.7.0+ installed**:
   ```bash
   podman --version
   # Should show: podman version 5.7.0 or higher
   ```

2. **fetchit binary built with Podman v5**:
   ```bash
   make build
   ls -lh fetchit
   # Should show recent build timestamp
   ```

3. **Test Git repository** (for change detection tests):
   - Create a test GitHub/GitLab repository
   - Populate with sample configurations

## Test Scenarios

### 1. Git Repository Change Detection (T073)

**Configuration**: `examples/readme-config.yaml`

**Test Steps**:
1. Create config pointing to test repository
2. Start fetchit with config
3. Make a commit to the repository
4. Verify fetchit detects change automatically
5. Verify container/pod is redeployed

**Expected Result**: Automatic redeployment on git push

**Validation**:
```bash
# Check fetchit logs
podman logs fetchit

# Verify redeployment
podman ps | grep <container-name>
```

---

### 2. Multi-Engine Configuration (T074)

**Configuration**: Create test config with all engines:

```yaml
targetConfigs:
  - url: <your-repo-url>
    branch: main
    filetransfer:
      - name: test-ft
        targetPath: /tmp
    raw:
      - name: test-raw
        image: docker.io/nginx:latest
    kube:
      - name: test-kube
        targetPath: manifests/
    systemd:
      - name: test-systemd
        targetPath: systemd/
```

**Test Steps**:
1. Deploy configuration
2. Make changes in each mechanism's directory
3. Commit and push
4. Verify each mechanism responds correctly

**Expected Result**: All 4 mechanisms work independently

---

### 3. Configuration File Reload (T075)

**Test Steps**:
1. Start fetchit with initial config
2. Modify config file without restarting container
3. Wait for automatic reload (check logs)
4. Verify new configuration is active

**Expected Result**: Config reloads without restart

**Validation**:
```bash
# Watch logs for reload message
podman logs -f fetchit | grep -i "reload"
```

---

### 4. Disconnected Operation Mode (T076)

**Configuration**: `examples/full-suite-disconnected.yaml`

**Test Steps**:
1. Create tar archive of git repository:
   ```bash
   git archive --format=tar -o /tmp/repo.tar HEAD
   ```
2. Configure fetchit for disconnected mode
3. Point to local archive instead of remote URL
4. Verify fetchit processes archive correctly

**Expected Result**: Works without network access to git

---

### 5. PAT Token Authentication (T077)

**Configuration**: `examples/pat-testing-config.yaml`

**Test Steps**:
1. Create GitHub/GitLab Personal Access Token
2. Configure fetchit with token:
   ```yaml
   targetConfigs:
     - url: https://github.com/private/repo
       branch: main
       token: <your-pat-token>
   ```
3. Verify access to private repository

**Expected Result**: Can clone private repository

---

### 6. SSH Key Authentication (T078)

**Test Steps**:
1. Generate SSH key pair:
   ```bash
   ssh-keygen -t ed25519 -f ~/.ssh/fetchit_test
   ```
2. Add public key to GitHub/GitLab
3. Configure fetchit with SSH URL:
   ```yaml
   targetConfigs:
     - url: git@github.com:user/repo.git
       sshkey: /path/to/fetchit_test
   ```
4. Verify SSH clone works

**Expected Result**: Can clone via SSH

---

### 7. Podman Secret Authentication (T079)

**Configuration**: `examples/podman-secret-raw.yaml`

**Test Steps**:
1. Create Podman secret:
   ```bash
   echo "my-secret-token" | podman secret create github-token -
   ```
2. Configure fetchit to use secret:
   ```yaml
   targetConfigs:
     - url: https://github.com/private/repo
       secret: github-token
   ```
3. Verify secret is used for authentication

**Expected Result**: Uses Podman secret for auth

---

### 8. Raw Container Deployment with Capabilities (T080)

**Configuration**: `examples/raw-config.yaml`

**Test Steps**:
1. Create raw container config with capabilities:
   ```yaml
   raw:
     - name: nginx-test
       image: docker.io/nginx:latest
       capAdd: ["NET_ADMIN", "SYS_TIME"]
       ports:
         - hostPort: 8080
           containerPort: 80
   ```
2. Deploy configuration
3. Verify container has requested capabilities:
   ```bash
   podman inspect nginx-test | grep -i cap
   ```

**Expected Result**: Container runs with specified capabilities

---

### 9. Kubernetes Pod Deployment (T081)

**Configuration**: `examples/kube-play-config.yaml`

**Test Steps**:
1. Create Kubernetes YAML manifest
2. Configure fetchit to watch manifest directory
3. Commit manifest to repository
4. Verify pod is created via `podman play kube`

**Expected Result**: Pod created from Kubernetes YAML

**Validation**:
```bash
podman pod list
podman ps --pod
```

---

### 10. Systemd Unit Deployment and Enabling (T082)

**Configuration**: `examples/systemd-config.yaml`

**Test Steps**:
1. Create systemd unit file in repository
2. Configure fetchit to manage systemd units
3. Commit unit file
4. Verify unit is deployed and enabled:
   ```bash
   systemctl --user list-unit-files | grep <unit-name>
   systemctl --user status <unit-name>
   ```

**Expected Result**: Systemd unit deployed and enabled

---

### 11. File Transfer Operations (T083)

**Configuration**: `examples/filetransfer-config.yaml`

**Test Steps**:
1. Create files in repository to transfer
2. Configure fetchit file transfer:
   ```yaml
   filetransfer:
     - name: config-sync
       targetPath: /etc/myapp/
       destination: /host/path/
   ```
3. Commit files
4. Verify files transferred to destination:
   ```bash
   ls -la /host/path/
   ```

**Expected Result**: Files copied to target directory

---

### 12. Ansible Playbook Execution (T084)

**Configuration**: `examples/ansible.yaml`

**Test Steps**:
1. Create Ansible playbook in repository
2. Configure SSH directory for Ansible
3. Configure fetchit ansible mechanism
4. Commit playbook
5. Verify playbook execution in logs

**Expected Result**: Ansible playbook executes successfully

---

## Regression Testing Checklist

After all functional tests, verify:

- [ ] **No configuration file format changes** - Existing configs still work
- [ ] **Container names unchanged** - No naming conflicts
- [ ] **Volume mounts work** - Named volumes accessible
- [ ] **Network connectivity** - Containers can communicate
- [ ] **Log output format** - Logs are readable and consistent
- [ ] **Error messages** - Clear error messages on failures
- [ ] **Restart policy** - Containers restart as configured
- [ ] **Cleanup operations** - Old containers/pods removed properly

## Performance Validation

Compare Podman v5 vs v4 performance:

1. **Container start time**:
   ```bash
   time podman run --rm docker.io/nginx:latest echo "test"
   ```

2. **Image pull time**:
   ```bash
   time podman pull docker.io/nginx:latest
   ```

3. **Repository clone time**:
   ```bash
   # Check fetchit logs for clone duration
   ```

4. **Overall cycle time** (detect → deploy):
   - Measure time from git push to container running

**Expected**: Similar or better performance than Podman v4

## Known Issues / Gotchas

### Podman v5 Specific

1. **CNI → Netavark Migration**:
   - If upgrading existing Podman installation, may need `podman system reset`
   - Network configurations might need updating

2. **Kernel Requirement**:
   - Linux Kernel 5.2+ required
   - Check: `uname -r`

3. **--device with --privileged**:
   - Behavior changed - device flag no longer ignored

### Testing Tips

1. **Use separate test namespace**:
   ```bash
   podman system service --time=0 unix:///tmp/podman-test.sock
   ```

2. **Clean state between tests**:
   ```bash
   podman pod rm -af
   podman container prune -f
   podman volume prune -f
   ```

3. **Verbose logging**:
   - Add `--log-level=debug` to fetchit for detailed logs

## Test Results Template

```markdown
## Test Results - Podman v5 Functional Validation

**Date**: YYYY-MM-DD
**Tester**: Your Name
**Podman Version**: $(podman --version)
**Fetchit Build**: $(./fetchit --version)

### Test Summary
- Total Scenarios: 12
- Passed: X
- Failed: X
- Skipped: X

### Detailed Results

#### T073 - Git Change Detection
- Status: PASS/FAIL
- Notes: ...

#### T074 - Multi-Engine
- Status: PASS/FAIL
- Notes: ...

[Continue for all tests...]

### Regressions Found
- None / List any issues

### Performance Observations
- Container start: X seconds (was Y in v4)
- Image pull: X seconds (was Y in v4)

### Recommendations
- ...
```

## Documentation

Record all test results in:
- `specs/001-podman-v4-upgrade/functional-test-results.md`

Include:
- Screenshots of successful deployments
- Log excerpts showing key operations
- Any errors encountered and resolutions
