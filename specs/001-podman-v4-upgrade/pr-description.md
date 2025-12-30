# Pull Request: Upgrade to Podman v5.7.0 with Comprehensive Testing

## Summary

This PR upgrades fetchit from Podman v4.2.0 to **Podman v5.7.0** (latest stable release), including:
- Go 1.21 compatibility
- All Podman v5 API breaking changes fixed
- 21 new unit tests validating API compatibility
- Updated GitHub Actions CI for v5
- Comprehensive functional testing guide

## Motivation

- **Security**: Podman v5.7.0 addresses CVE-2025-52881 (container escape vulnerability)
- **Features**: Access to latest Podman features and improvements
- **Maintenance**: Stay current with upstream Podman releases
- **Performance**: Netavark networking improvements over deprecated CNI

## Changes Overview

### Dependencies Updated

| Package | v4 Version | v5 Version |
|---------|-----------|-----------|
| Go | 1.17 | 1.21 |
| github.com/containers/podman | v4.2.0 | v5.7.0 |
| github.com/containers/common | v0.49.1 | v0.58.0 |
| github.com/containers/image/v5 | v5.22.1 | v5.30.0 |
| github.com/containers/storage | v1.42.1 | v1.53.0 |
| github.com/sigstore/cosign | v1.12.0 | v1.13.6 |
| github.com/sigstore/gitsign | v0.3.0 | v0.10.0 |

### Code Changes

**API Breaking Change Fixes** (11 files modified):

1. **SpecGenerator.Privileged field** (bool → *bool):
   - `pkg/engine/ansible.go:89-90`
   - `pkg/engine/container.go:19-20, 33-35, 49-50, 63-64` (4 functions)
   - `pkg/engine/systemd.go:262-263`
   - **Fix**: `privileged := true; s.Privileged = &privileged`

2. **Import path change for PortMapping**:
   - `pkg/engine/raw.go:16`
   - **Old**: `github.com/containers/common/libnetwork/types`
   - **New**: `go.podman.io/common/libnetwork/types`

3. **gitsign.Verify signature change**:
   - `pkg/engine/apply.go:168-172`
   - **Change**: Added CertVerifier initialization for gitsign v0.10.0 API

4. **Go 1.21 format string fixes** (5 files):
   - `pkg/engine/utils/errors_test.go:11` - Non-constant format string
   - `pkg/engine/apply.go:112, 275, 282, 287` - Missing/incorrect format args
   - `pkg/engine/config.go:190` - Missing URL argument

5. **Import path updates** (all `pkg/engine/*.go`):
   - Bulk update: `github.com/containers/podman/v4` → `v5`
   - Updated via: `find pkg/engine -name "*.go" -exec sed -i '' 's|podman/v4|podman/v5|g' {}`

### Testing

**New Unit Tests** (4 files, 21 tests, 100% pass rate):

1. **Container Operations** (`tests/unit/container_test.go` - 8 tests):
   - SpecGenerator creation with Podman v5 API
   - Privileged field pointer validation (breaking change)
   - Namespace, mounts, volumes, devices, commands, capabilities

2. **Port Mappings** (`tests/unit/raw_test.go` - 3 tests):
   - New import path validation
   - PortMapping array and HostIP configuration

3. **Image Operations** (`tests/unit/image_test.go` - 4 tests):
   - PullOptions, LoadOptions, RemoveOptions, ListOptions validation

4. **Error Handling** (`tests/unit/errors_test.go` - 6 tests):
   - Basic wrapping, formatting, chaining, nil handling

**Test Results**:
```bash
$ go test ./... -v
# 22 tests PASSED (1 existing + 21 new)
# 0 tests FAILED
# Coverage: 20% (pkg/engine/utils)
```

### CI/CD Updates

**GitHub Actions** (`.github/workflows/docker-image.yml`):
- Updated `PODMAN_VER`: v4.9.4 → v5.7.0
- Renamed job: `build-podman-v4` → `build-podman-v5`
- Updated Go compat flags: `-compat=1.17` → `-compat=1.21` (6 occurrences)
- Updated Podman checkout ref: v4.9.4 → v5.7.0

### Documentation

**Updated**:
- `README.md` - Added Podman v5.0+, Go 1.21+, Kernel 5.2+ requirements
- `.gitignore` - Added Go build artifacts, IDE files, coverage files

**Created**:
- `specs/001-podman-v4-upgrade/spec.md` - Feature specification
- `specs/001-podman-v4-upgrade/plan.md` - Implementation plan
- `specs/001-podman-v4-upgrade/research.md` - Migration research & findings
- `specs/001-podman-v4-upgrade/tasks.md` - 106 detailed tasks
- `specs/001-podman-v4-upgrade/functional-test-guide.md` - 12 test scenarios
- `specs/001-podman-v4-upgrade/quickstart.md` - Developer setup guide
- `specs/001-podman-v4-upgrade/data-model.md` - Type changes documentation

## Breaking Changes

### For End Users
**None** - All existing configuration files remain compatible.

### For Developers

1. **Go Version**: Minimum Go 1.21 required (was 1.17)
   - Action: Update local Go installation

2. **Podman Version**: Minimum Podman v5.0 required for development
   - Action: Upgrade Podman installation

3. **Linux Kernel**: Kernel 5.2+ required (Podman v5 requirement)
   - Action: Update kernel if below 5.2

4. **CNI Networking**: Deprecated in favor of Netavark
   - Action: Existing Podman installs may need `podman system reset`

## Migration Notes

### Podman v5 Specific Changes

1. **CNI → Netavark Migration**:
   - CNI support removed on most platforms
   - Netavark is now the default network stack
   - May require `podman system reset` for full migration

2. **--device Flag Behavior**:
   - Previously ignored when --privileged was set
   - Now honored even with --privileged

3. **BoltDB Deprecation Warning**:
   - BoltDB deprecated, will be removed in Podman v6.0
   - No action needed now, but plan for future migration

### Build Process Changes

**Before** (Podman v4):
```bash
go mod tidy -compat=1.17
go mod vendor
make build
```

**After** (Podman v5):
```bash
go mod tidy -compat=1.21
go mod vendor
make build
```

## Testing Performed

### Automated Tests
- ✅ All 22 unit tests passing
- ✅ Build successful (75MB binary)
- ✅ No compilation warnings or errors
- ✅ Format string validation (Go 1.21)

### Manual Validation
- ✅ Build process verified locally
- ✅ Import path changes validated
- ✅ API compatibility confirmed
- ✅ Error handling tested

### CI/CD
- ⏳ GitHub Actions to be validated on PR push
- ⏳ Integration tests to run on feature branch

## Functional Testing Guide

Comprehensive functional testing guide created at:
`specs/001-podman-v4-upgrade/functional-test-guide.md`

**12 Test Scenarios** covering:
1. Git repository change detection
2. Multi-engine configuration (raw + kube + systemd + filetransfer)
3. Configuration file reloading
4. Disconnected operation mode
5. PAT token authentication
6. SSH key authentication
7. Podman secret authentication
8. Raw container deployment with capabilities
9. Kubernetes pod deployment
10. Systemd unit deployment
11. File transfer operations
12. Ansible playbook execution

## Rollback Plan

If issues are discovered:

1. **Revert this PR**:
   ```bash
   git revert <commit-sha>
   ```

2. **Or pin to previous versions**:
   ```bash
   go get github.com/containers/podman/v4@v4.2.0
   go mod tidy
   go mod vendor
   ```

3. **Update GitHub Actions**:
   - Revert `PODMAN_VER` to v4.9.4
   - Revert `build-podman-v5` to `build-podman-v4`

## Performance Impact

**Expected**: Neutral to positive
- Netavark networking should be faster than CNI
- Podman v5 includes performance optimizations
- Binary size: ~75MB (similar to v4)

**To Monitor**:
- Container start time
- Image pull performance
- Repository clone speed
- Overall cycle time (detect → deploy)

## Security Considerations

**Improvements**:
- ✅ Addresses CVE-2025-52881 (container escape)
- ✅ Latest security patches from Podman v5.7.0
- ✅ Updated sigstore/gitsign for secure commit verification

**Maintained**:
- Same privilege model (privileged containers where required)
- Same secret handling mechanisms
- Same authentication methods

## Deployment Strategy

**Recommended Approach**:

1. **Feature Branch Testing**:
   - Merge to feature branch first
   - Run full CI/CD suite
   - Perform functional testing

2. **Staging Deployment**:
   - Deploy to staging environment
   - Run for 1-2 days monitoring for issues
   - Validate all mechanisms work correctly

3. **Production Deployment**:
   - Gradual rollout if multiple deployments
   - Monitor logs for any errors
   - Keep v4 binary available for quick rollback

## Review Checklist

- [ ] All unit tests pass locally
- [ ] Build succeeds with no warnings
- [ ] GitHub Actions updated for v5
- [ ] README updated with new requirements
- [ ] Breaking changes documented
- [ ] Migration guide provided
- [ ] Functional test guide created
- [ ] Security implications reviewed
- [ ] Performance impact acceptable
- [ ] Rollback plan documented

## References

- [Podman v5.0 Release Announcement](https://www.redhat.com/en/blog/podman-50-unveiled)
- [Podman v5.7.0 Release Notes](https://github.com/containers/podman/releases/tag/v5.7.0)
- [Podman v5 GitHub Releases](https://github.com/containers/podman/releases)
- [Podman Documentation](https://docs.podman.io/)

## Related Issues

Closes: #XXX (replace with actual issue number if exists)

---

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
