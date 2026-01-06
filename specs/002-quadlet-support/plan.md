# Implementation Plan: Quadlet Container Deployment (Podman v5.7.0)

**Branch**: `002-quadlet-support` | **Date**: 2026-01-06 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/002-quadlet-support/spec.md`

## Summary

Implement comprehensive Podman Quadlet support for fetchit, enabling declarative container deployment using all eight Podlet file types supported by Podman v5.7.0. The implementation will extend the existing Quadlet engine in `pkg/engine/quadlet.go` to support `.pod`, `.build`, `.image`, and `.artifact` files (currently only `.container`, `.volume`, `.network`, and `.kube` are supported), add v5.7.0-specific configuration options (HttpProxy, StopTimeout, BuildArg, IgnoreFile), and leverage the existing file transfer mechanism from `pkg/engine/filetransfer.go` for maximum code reuse.

## Technical Context

**Language/Version**: Go 1.24.2
**Primary Dependencies**:
- `github.com/containers/podman/v5` v5.7.0 (Quadlet support with all file types)
- `github.com/go-git/go-git/v5` v5.14.0 (Git repository monitoring)
- `github.com/opencontainers/runtime-spec` v1.2.1 (OCI spec generation)
- `github.com/spf13/viper` v1.21.0 (Configuration management)

**Storage**: Git repositories (monitored for Quadlet file changes), Host filesystem (`/etc/containers/systemd/` for rootful, `~/.config/containers/systemd/` for rootless)
**Testing**: Go test framework, GitHub Actions CI with Podman v5.7.0
**Target Platform**: Linux with systemd and Podman v5.7.0+
**Project Type**: Single Go project with CLI tool
**Performance Goals**: Process Quadlet file changes within same polling interval as existing methods (typically 1-5 minutes), daemon-reload < 2 seconds
**Constraints**:
- Must maintain backward compatibility with existing systemd method during deprecation period
- Must work in both rootful and rootless modes
- Must pass all GitHub Actions tests before merging
- Quadlet files must be processed using existing file transfer mechanism

**Scale/Scope**:
- Support 8 Quadlet file types (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`)
- Handle multi-file deployments with dependency ordering
- Support both rootful and rootless deployments concurrently

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: The project does not have a ratified constitution file yet. Proceeding with standard Go project best practices:

✅ **Go Conventions**: Follow standard Go code organization, error handling, and testing practices
✅ **Existing Patterns**: Reuse existing patterns from `pkg/engine/` for consistency (Method interface, file transfer mechanism, systemd integration)
✅ **Test Coverage**: Comprehensive GitHub Actions workflows already exist for Quadlet validation (quadlet-validate, quadlet-user-validate, quadlet-volume-network-validate, quadlet-kube-validate)
✅ **Documentation**: Examples required for all supported file types
✅ **Backward Compatibility**: Legacy systemd method must remain functional during deprecation period

**No violations** - Implementation extends existing patterns without introducing complexity.

## Project Structure

### Documentation (this feature)

```text
specs/002-quadlet-support/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
│   └── quadlet-api.md   # Quadlet Method interface contract
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
pkg/engine/
├── quadlet.go           # EXISTING - Quadlet method implementation (NEEDS EXTENSION)
├── filetransfer.go      # EXISTING - File transfer mechanism (REUSED)
├── types.go             # EXISTING - Method interface (UNCHANGED)
├── common.go            # EXISTING - Common method utilities (MAY EXTEND)
├── systemd.go           # EXISTING - systemd integration (REFERENCE FOR PATTERNS)
├── apply.go             # EXISTING - Change application logic (REUSED)
├── config.go            # EXISTING - Configuration parsing (UNCHANGED)
└── utils/
    └── errors.go        # EXISTING - Error wrapping utilities (REUSED)

examples/quadlet/
├── simple.container      # EXISTING - Basic container example
├── httpd.container       # EXISTING - Container with networking
├── httpd.volume          # EXISTING - Volume definition
├── httpd.network         # EXISTING - Network definition
├── colors.kube           # EXISTING - Kubernetes YAML deployment
├── httpd.pod             # NEW - Multi-container pod example
├── webapp.build          # NEW - Image build example
├── nginx.image           # NEW - Image pull example
└── artifact.artifact     # NEW - OCI artifact example

examples/
├── quadlet-config.yaml          # EXISTING - Basic Quadlet configuration
├── quadlet-rootless.yaml        # EXISTING - Rootless configuration
├── quadlet-pod.yaml             # NEW - Pod deployment configuration
├── quadlet-build.yaml           # NEW - Build configuration
├── quadlet-image.yaml           # NEW - Image pull configuration
└── quadlet-artifact.yaml        # NEW - Artifact configuration

.github/workflows/
└── docker-image.yml      # EXISTING - Contains quadlet-validate tests (NEEDS EXTENSION)
```

**Structure Decision**: Single project structure maintained. New Quadlet file type support will be added to the existing `pkg/engine/quadlet.go` file, extending the current implementation. GitHub Actions workflows will be extended with new test jobs for `.pod`, `.build`, `.image`, and `.artifact` files.

## Complexity Tracking

No violations requiring justification. This implementation:
- Extends existing Quadlet support (already present in codebase)
- Reuses existing file transfer mechanism (proven pattern)
- Follows established Method interface pattern (consistency)
- Adds tests to existing GitHub Actions workflow (incremental)

## Architecture Decisions

### Decision 1: Extend Existing Quadlet Implementation

**Decision**: Extend `pkg/engine/quadlet.go` rather than create new method types for each Quadlet file type.

**Rationale**:
- Current implementation already handles `.container`, `.volume`, `.network`, `.kube`
- All Quadlet file types share the same processing flow: file transfer → daemon-reload → service management
- Reduces code duplication and maintenance burden
- Maintains single configuration point for Quadlet deployments

**Implementation**:
1. Update `tags` array in `Process()` method to include all 8 file types
2. Extend `deriveServiceName()` function to handle `.pod`, `.build`, `.image`, `.artifact`
3. Add service name derivation rules per Podman v5.7.0 specifications

### Decision 2: Reuse FileTransfer Mechanism

**Decision**: Use existing `FileTransfer.fileTransferPodman()` method for all Quadlet file operations.

**Rationale**:
- Already implemented and tested pattern (see line 430-434, 443 in quadlet.go)
- Creates temporary containers with bind mounts to access host filesystem
- Handles both rootful and rootless modes correctly
- Proven approach used by systemd method as well

**Current Implementation**:
```go
ft := &FileTransfer{
    CommonMethod: CommonMethod{
        Name: q.Name,
    },
}
if err := ft.fileTransferPodman(ctx, conn, path, paths.InputDirectory, nil); err != nil {
    return fmt.Errorf("failed to copy Quadlet file: %w", err)
}
```

### Decision 3: Incremental GitHub Actions Test Coverage

**Decision**: Add new test jobs for each new Quadlet file type following existing pattern.

**Rationale**:
- Existing tests demonstrate proven pattern: quadlet-validate, quadlet-user-validate, quadlet-volume-network-validate, quadlet-kube-validate
- Each test job validates end-to-end: file placement → daemon-reload → service generation → service start
- Tests run in CI pipeline, blocking merges if failures occur
- Comprehensive coverage requirement from user specification

**New Test Jobs Required**:
- `quadlet-pod-validate`: Test `.pod` file deployment with multi-container pods
- `quadlet-build-validate`: Test `.build` file with BuildArg and IgnoreFile
- `quadlet-image-validate`: Test `.image` file pulling from registries
- `quadlet-artifact-validate`: Test `.artifact` file OCI artifact management

### Decision 4: Configuration Options Support

**Decision**: Support v5.7.0 configuration options transparently without code changes.

**Rationale**:
- Quadlet files are copied verbatim to systemd directories
- Podman's systemd generator reads and interprets all configuration options
- fetchit acts as file delivery mechanism, not parser
- HttpProxy, StopTimeout, BuildArg, IgnoreFile are processed by Podman, not fetchit

**No Implementation Required** for configuration options - they work automatically once files are placed.

## Phase 0: Outline & Research

### Research Tasks

1. **Podman v5.7.0 Quadlet File Types**
   - Research: Comprehensive analysis of all 8 Quadlet file types
   - Deliverable: Document file type specifications, service naming conventions, systemd dependency patterns
   - Status: **COMPLETED** (see spec.md)

2. **Service Name Derivation Rules**
   - Research: How Podman derives systemd service names from each file type
   - Deliverable: Mapping table for `deriveServiceName()` function extension
   - Status: **NEEDS CLARIFICATION** - Need to verify exact service naming for `.build`, `.image`, `.artifact` files

3. **File Transfer Pattern for Builds**
   - Research: How `.build` files access build context from Git repository
   - Deliverable: Pattern for ensuring Dockerfile and build context availability
   - Status: **NEEDS CLARIFICATION** - Build context path resolution strategy

4. **OCI Artifact Registry Interaction**
   - Research: How Podman handles `.artifact` files, registry authentication
   - Deliverable: Authentication and registry access pattern documentation
   - Status: **NEEDS CLARIFICATION** - Artifact registry configuration approach

5. **GitHub Actions Test Pattern**
   - Research: Examine existing quadlet test jobs for pattern consistency
   - Deliverable: Template for new test jobs (pod, build, image, artifact)
   - Status: **NEEDS CLARIFICATION** - Specific validation criteria for each new file type

### Dependencies & Best Practices

1. **Go-git Integration** (EXISTING)
   - Pattern: Use `applyChanges()` from `apply.go` for change detection
   - Reuse: `runChanges()` for executing file operations sequentially

2. **Podman Go SDK Usage** (EXISTING)
   - Pattern: Use `specgen.NewSpecGenerator()` for creating temporary containers
   - Reuse: `createAndStartContainer()`, `waitAndRemoveContainer()` helpers

3. **systemd Integration** (EXISTING)
   - Pattern: Use container-based systemctl execution (see `systemd.go`)
   - Reuse: daemon-reload, start, stop, restart service management functions

4. **Error Handling** (EXISTING)
   - Pattern: Wrap errors with context using `utils.WrapErr()`
   - Logging: Use structured logging with `logger.Infof/Errorf`

## Phase 1: Design & Contracts

### Data Model

**Primary Entity**: QuadletFileMetadata (EXISTING, line 51-69 in quadlet.go)

**Extensions Required**:
- No structural changes to existing entities
- `FileType` enum needs 4 new values: `QuadletPod`, `QuadletBuild`, `QuadletImage`, `QuadletArtifact`

**Key Relationships**:
- QuadletFileMetadata → Git Change (1:1)
- QuadletFileMetadata → systemd Service Unit (1:1)
- Quadlet method → FileTransfer method (reuse composition)

### API Contracts

**Method Interface** (EXISTING - No Changes Required):
```go
type Method interface {
    GetName() string
    GetKind() string
    GetTarget() *Target
    Process(ctx context.Context, conn context.Context, skew int)
    Apply(ctx context.Context, conn context.Context, currentState plumbing.Hash, desiredState plumbing.Hash, tags *[]string) error
    MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
}
```

**Quadlet Configuration** (EXISTING - No Changes Required):
```go
type Quadlet struct {
    CommonMethod `mapstructure:",squash"`
    Root         bool `mapstructure:"root"`     // Rootful vs rootless
    Enable       bool `mapstructure:"enable"`   // Enable services
    Restart      bool `mapstructure:"restart"`  // Restart on update
}
```

### Implementation Phases

**Phase 1.1**: Extend Quadlet File Type Support
- Update `tags` array: `[]string{".container", ".volume", ".network", ".pod", ".build", ".image", ".artifact", ".kube"}`
- Extend `deriveServiceName()` with new file type mappings (based on research findings)
- Update `QuadletFileType` enum

**Phase 1.2**: Create Examples
- Add 4 new example Quadlet files (`.pod`, `.build`, `.image`, `.artifact`)
- Add 4 new example configurations demonstrating each file type
- Document v5.7.0-specific features in example files (HttpProxy, StopTimeout, BuildArg, IgnoreFile)

**Phase 1.3**: GitHub Actions Tests
- Create `quadlet-pod-validate` job
- Create `quadlet-build-validate` job
- Create `quadlet-image-validate` job
- Create `quadlet-artifact-validate` job
- Each job follows existing pattern: file placement → daemon-reload → service verification

**Phase 1.4**: Documentation
- Update README.md with Quadlet v5.7.0 capabilities
- Create quickstart.md with migration guide from systemd method to Quadlet
- Document all 8 file types with practical examples

## Testing Strategy

### Unit Tests (Go)
- Test `deriveServiceName()` for all 8 file types
- Test `GetQuadletDirectory()` for rootful/rootless path resolution
- Test `determineChangeType()` for create/update/delete/rename operations

### Integration Tests (GitHub Actions)
- **EXISTING**: quadlet-validate (`.container`)
- **EXISTING**: quadlet-user-validate (rootless `.container`)
- **EXISTING**: quadlet-volume-network-validate (`.volume`, `.network`)
- **EXISTING**: quadlet-kube-validate (`.kube`)
- **NEW**: quadlet-pod-validate (`.pod` with StopTimeout)
- **NEW**: quadlet-build-validate (`.build` with BuildArg, IgnoreFile)
- **NEW**: quadlet-image-validate (`.image` pull operation)
- **NEW**: quadlet-artifact-validate (`.artifact` OCI artifact)

### Test Success Criteria
- All existing tests continue to pass
- New tests validate full lifecycle: file transfer → daemon-reload → service start → resource verification
- Tests run in both rootful and rootless modes where applicable
- Tests verify v5.7.0-specific features work correctly

## Migration Path

### From Existing Systemd Method

**User Impact**: None (backward compatible)

**Migration Steps**:
1. Users continue using systemd method (deprecated but functional)
2. Users read quickstart.md migration guide
3. Users create Quadlet files for new deployments
4. Users gradually convert systemd services to Quadlet `.container` files
5. fetchit supports both methods simultaneously

**No Breaking Changes**: Existing systemd method remains fully functional.

## Backward Compatibility Guarantee

### CRITICAL: Zero Breaking Changes Policy

This implementation **MUST NOT** break any existing deployments or engines. All changes are purely additive.

**What WILL NOT Change**:
- ✅ Method interface (`pkg/engine/types.go`) - No modifications
- ✅ Existing method implementations (`kube.go`, `ansible.go`, `raw.go`) - No modifications
- ✅ Configuration schema - Quadlet fields are optional additions only
- ✅ Existing Quadlet support (`.container`, `.volume`, `.network`, `.kube`) - No changes to behavior
- ✅ Git change detection logic (`apply.go`) - Only reused, not modified

**What MAY Change** (If Supporting Quadlet):
- ⚠️ `pkg/engine/systemd.go` - MAY be modified to support Quadlet integration IF systemd-validate tests still pass
- ⚠️ `pkg/engine/filetransfer.go` - MAY be modified to support Quadlet integration IF filetransfer-validate tests still pass

**What WILL Change** (Additive):
- ➕ `pkg/engine/quadlet.go` - Add 3 cases to `deriveServiceName()` switch statement
- ➕ `pkg/engine/quadlet.go` - Update `tags` array from 4 to 8 file types
- ➕ `examples/quadlet/` - Add 4 new example files (.pod, .build, .image, .artifact)
- ➕ `examples/` - Add 4 new configuration files
- ➕ `.github/workflows/docker-image.yml` - Add 4 new test jobs (no modification to existing jobs)

**Validation Strategy**:
1. **Pre-Implementation**: Review all existing engine code - identify if systemd.go or filetransfer.go modifications would help
2. **During Implementation**: Extend `pkg/engine/quadlet.go` primarily; modify systemd.go/filetransfer.go only if beneficial
3. **Testing**: Run ALL existing CI tests - must pass to validate backward compatibility
4. **Post-Implementation**: Verify existing deployments continue working

**Rollback Procedure**:
1. Revert changes to `pkg/engine/quadlet.go` (deriveServiceName, tags array)
2. Revert any changes to `pkg/engine/systemd.go` (if modified)
3. Revert any changes to `pkg/engine/filetransfer.go` (if modified)
4. Remove new example files (examples/quadlet/{httpd.pod, webapp.build, nginx.image, artifact.artifact})
5. Remove new CI test jobs from `.github/workflows/docker-image.yml`
6. Existing deployments unaffected - no data loss

**Concurrent Method Support**:
- Users can run systemd + quadlet simultaneously (different directories: `/etc/systemd/system/` vs `/etc/containers/systemd/`)
- Users can run kube + quadlet simultaneously (different resource types)
- Users can run multiple quadlet targets with different configurations

## Risk Assessment

### Low Risk (Mitigated)
- ✅ Reusing proven file transfer mechanism (no modifications)
- ✅ Extending existing Quadlet implementation (purely additive)
- ✅ Following established patterns (Method interface unchanged)
- ✅ Comprehensive test coverage via GitHub Actions
- ✅ Backward compatibility guaranteed (FR-026 to FR-035)
- ✅ Rollback procedure documented

### Medium Risk (Addressed)
- ⚠️ `.build` file support - **RESOLVED**: research.md confirms file transfer handles build context automatically
- ⚠️ `.artifact` file support - **RESOLVED**: research.md confirms Podman handles authentication, fetchit just delivers files
- ⚠️ Service naming conventions - **RESOLVED**: research.md documents exact Podman v5.7.0 naming rules

### Zero Risk to Existing Deployments
- ✅ No modifications to other methods (systemd, kube, ansible, filetransfer, raw)
- ✅ No modifications to Method interface
- ✅ Existing Quadlet deployments continue working (just adding more file types)
- ✅ All existing tests must pass before merge

### Mitigation Strategies
- Phase 0 research resolved all NEEDS CLARIFICATION items
- GitHub Actions tests will catch integration issues early
- Examples will demonstrate correct usage patterns for all file types
- Documentation will guide users through adoption
- **Backward compatibility validation** in CI (all existing tests must pass)

## Deliverables

### Phase 0 (Research)
- [x] research.md with all NEEDS CLARIFICATION resolved
- [x] Service naming convention mappings
- [x] Build context resolution strategy
- [x] Artifact registry interaction patterns
- [x] GitHub Actions test templates

### Phase 1 (Design)
- [x] data-model.md with entity extensions
- [x] contracts/quadlet-api.md with Method interface verification
- [x] quickstart.md with migration guide and examples
- [x] Agent context updated (Claude/agent-specific file)

### Phase 2 (Implementation - handled by /speckit.tasks)
- [ ] Extended Quadlet file type support in quadlet.go
- [ ] New example Quadlet files (4 files)
- [ ] New example configurations (4 YAML files)
- [ ] New GitHub Actions test jobs (4 jobs)
- [ ] Updated documentation (README, examples)
- [ ] All tests passing in CI pipeline

## Success Criteria

Implementation is complete when:
1. ✅ All 8 Quadlet file types supported (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`)
2. ✅ Existing tests continue to pass
3. ✅ 4 new GitHub Actions test jobs added and passing
4. ✅ Examples exist for all file types with v5.7.0 features demonstrated
5. ✅ quickstart.md provides clear migration path from systemd method
6. ✅ No changes required to Method interface or configuration schema
7. ✅ File transfer mechanism reused successfully
8. ✅ Both rootful and rootless modes work correctly

## Next Steps

1. Run `.specify/scripts/bash/update-agent-context.sh claude` to update agent context
2. Execute `/speckit.tasks` to generate implementation tasks from this plan
3. Begin implementation following task order in tasks.md
4. Submit PR only after all GitHub Actions tests pass
