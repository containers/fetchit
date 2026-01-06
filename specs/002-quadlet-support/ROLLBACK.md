# Rollback Procedure: Quadlet v5.7.0 Support

**Feature**: Quadlet Container Deployment (Podman v5.7.0)
**Branch**: `002-quadlet-support`
**Date**: 2026-01-06

## Purpose

This document provides step-by-step instructions for rolling back the Quadlet v5.7.0 file type extension if issues arise after deployment.

## Scope of Changes

The following files were modified or created in this feature:

### Modified Files
- `pkg/engine/quadlet.go` - Extended to support `.build`, `.image`, `.artifact` file types
- `README.md` - Updated to document Quadlet v5.7.0 support
- `examples/quadlet/README.md` - Updated to document all 8 file types

### Created Files
- `examples/quadlet/httpd.pod` - Pod example
- `examples/quadlet/webapp.build` - Build example
- `examples/quadlet/Dockerfile` - Dockerfile for build example
- `examples/quadlet/.dockerignore` - Build context ignore file
- `examples/quadlet/nginx.image` - Image pull example
- `examples/quadlet/artifact.artifact` - OCI artifact example
- `examples/quadlet-pod.yaml` - Pod configuration example
- `examples/quadlet-build.yaml` - Build configuration example
- `examples/quadlet-image.yaml` - Image configuration example
- `examples/quadlet-artifact.yaml` - Artifact configuration example

## Rollback Steps

### Step 1: Revert Code Changes

```bash
# Navigate to repository root
cd /Users/rcook/git/fetchit

# Identify the merge commit for this feature
git log --oneline --grep="002-quadlet-support" | head -5

# Option A: Revert specific file changes (preferred)
git checkout main -- pkg/engine/quadlet.go

# Option B: Revert the entire merge commit
# git revert -m 1 <merge-commit-sha>
```

### Step 2: Remove New Example Files

```bash
# Remove new Quadlet example files
rm -f examples/quadlet/httpd.pod
rm -f examples/quadlet/webapp.build
rm -f examples/quadlet/Dockerfile
rm -f examples/quadlet/.dockerignore
rm -f examples/quadlet/nginx.image
rm -f examples/quadlet/artifact.artifact

# Remove new configuration examples
rm -f examples/quadlet-pod.yaml
rm -f examples/quadlet-build.yaml
rm -f examples/quadlet-image.yaml
rm -f examples/quadlet-artifact.yaml

# Restore original README files
git checkout main -- examples/quadlet/README.md
git checkout main -- README.md
```

### Step 3: Rebuild and Test

```bash
# Clean Go build cache
go clean -cache -modcache

# Rebuild fetchit
go mod tidy
go mod vendor
podman build . --file Dockerfile --tag quay.io/fetchit/fetchit-amd:latest
podman tag quay.io/fetchit/fetchit-amd:latest quay.io/fetchit/fetchit:latest
```

### Step 4: Verify Existing Deployments

```bash
# Test existing .container files still work
sudo systemctl daemon-reload
systemctl list-units --all | grep -E '(simple|httpd)\.service'

# Verify existing quadlet deployments continue working
sudo systemctl status simple.service
podman ps | grep systemd-simple
```

### Step 5: Commit Rollback

```bash
# Stage all changes
git add .

# Commit rollback
git commit -m "Rollback: Revert Quadlet v5.7.0 file type extensions

This rollback reverts the following changes:
- Extended file type support (.build, .image, .artifact)
- New example files for v5.7.0 features
- Documentation updates

Reason: [Describe reason for rollback]

Reverts: [commit-sha or PR number]
"

# Push to rollback branch
git push origin rollback-002-quadlet-support
```

## Impact Assessment

### What Will Continue Working

✅ **Existing Quadlet deployments** - `.container`, `.volume`, `.network`, `.kube` files deployed before this feature
✅ **All other methods** - systemd, kube, ansible, filetransfer, raw methods remain unaffected
✅ **Running containers** - No impact on currently running containers managed by fetchit

### What Will Stop Working

❌ **New file types** - `.pod`, `.build`, `.image`, `.artifact` files will no longer be processed
❌ **New examples** - Example configurations for new file types will be removed
❌ **v5.7.0 features** - HttpProxy, StopTimeout, BuildArg, IgnoreFile documentation removed

### Data Loss

**No data loss expected** - Rollback only affects code and examples, not deployed containers or volumes.

## Validation After Rollback

### 1. Verify Code Rollback

```bash
# Check quadlet.go was reverted
git diff main -- pkg/engine/quadlet.go

# Verify tags array is back to original
grep -A 1 "tags :=" pkg/engine/quadlet.go
# Expected output: tags := []string{".container", ".volume", ".network", ".kube"}
```

### 2. Test Existing Functionality

```bash
# Test existing quadlet deployment
sudo cp examples/quadlet/simple.container /etc/containers/systemd/
sudo systemctl daemon-reload
sudo systemctl restart simple.service
sudo systemctl status simple.service
podman ps | grep systemd-simple
```

### 3. Run CI Tests

```bash
# Push rollback branch and verify CI passes
git push origin rollback-002-quadlet-support

# Monitor GitHub Actions:
# - quadlet-validate (must pass)
# - quadlet-user-validate (must pass)
# - quadlet-volume-network-validate (must pass)
# - quadlet-kube-validate (must pass)
# - systemd-validate (must pass)
# - kube-validate (must pass)
```

## Emergency Hotfix

If immediate rollback is needed in production:

```bash
# 1. Stop fetchit service
sudo systemctl stop fetchit.service

# 2. Remove problematic Quadlet files
sudo rm -f /etc/containers/systemd/*.{pod,build,image,artifact}

# 3. Reload systemd
sudo systemctl daemon-reload

# 4. Replace fetchit container with previous version
podman pull quay.io/fetchit/fetchit:<previous-tag>
podman tag quay.io/fetchit/fetchit:<previous-tag> quay.io/fetchit/fetchit:latest

# 5. Restart fetchit
sudo systemctl start fetchit.service

# 6. Verify existing deployments working
sudo systemctl status simple.service
```

## Prevention for Next Time

1. **Staging Testing** - Test new features in staging environment before production
2. **Gradual Rollout** - Deploy to subset of systems first
3. **Monitoring** - Monitor systemd service status and container health after deployment
4. **Backup** - Tag stable versions before deploying experimental features

## Contact

For issues or questions about rollback:
- GitHub Issues: https://github.com/containers/fetchit/issues
- Documentation: https://fetchit.readthedocs.io/

## Rollback Completed

- [ ] Code reverted to previous version
- [ ] New files removed
- [ ] Documentation restored
- [ ] Build successful
- [ ] Existing deployments validated
- [ ] CI tests passing
- [ ] Rollback committed and pushed
- [ ] Team notified

Date: ____________
Performed by: ____________
Reason: ____________
