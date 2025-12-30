# Implementation Plan: Podman v5 Dependency Upgrade with Comprehensive Testing

**Branch**: `001-podman-v4-upgrade` | **Date**: 2025-12-30 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-podman-v4-upgrade/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Upgrade all Go dependencies from Podman v4.2.0 to latest Podman v5.x versions, addressing breaking API changes while maintaining all existing fetchit functionality (container management, systemd integration, file transfer, Kubernetes YAML processing, and Ansible execution). Comprehensive testing strategy includes enhanced unit tests, updated GitHub Actions integration tests with Podman v5 builds, and manual functional validation to ensure zero regressions.

## Technical Context

**Language/Version**: Go 1.17 (current) → Go 1.21+ (required for Podman v5)
**Primary Dependencies**:
- github.com/containers/podman/v4 v4.2.0 → github.com/containers/podman/v5 v5.x (latest stable)
- github.com/containers/common v0.49.1 → version compatible with Podman v5
- github.com/containers/image/v5 → version compatible with Podman v5
- github.com/containers/storage → version compatible with Podman v5

**Storage**: N/A (fetchit manages containers, not application data)
**Testing**:
- Unit: Go testing framework (go test ./...)
- Integration: GitHub Actions with full Podman v5 build and deployment validation
- Functional: Manual testing of multi-engine configurations and edge cases

**Target Platform**: Linux (Ubuntu in CI, RHEL/Fedora/CentOS for production deployments)
**Project Type**: Single CLI application with container management library
**Performance Goals**:
- Container deployments complete within current timeframes (< 150 seconds per test scenario)
- Git repository polling and processing without degradation
- Unit tests complete in < 30 seconds total

**Constraints**:
- Zero regression in existing functionality
- Maintain backward compatibility with existing configuration files
- All GitHub Actions tests must pass (100% pass rate requirement)
- Code changes must accommodate Podman v5 API breaking changes

**Scale/Scope**:
- ~20 Go source files in pkg/engine/ requiring review and potential updates
- 1 main CLI entry point (cmd/fetchit/main.go)
- 40+ GitHub Actions integration test jobs to validate
- 8+ deployment mechanisms (raw, kube, systemd, filetransfer, ansible, clean, disconnected, image loader)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: N/A - No project constitution file exists yet. This is an existing open-source project (fetchit) with established patterns. We will follow existing project conventions:

1. **Testing Requirements**: All existing GitHub Actions tests must pass
2. **Code Quality**: Maintain existing code structure and patterns in pkg/engine/
3. **Backward Compatibility**: Configuration files must remain compatible
4. **Documentation**: Update dependency versions and API changes in code comments

**No violations detected** - This is a dependency upgrade maintaining existing architecture.

## Project Structure

### Documentation (this feature)

```text
specs/001-podman-v4-upgrade/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command) - minimal for this upgrade
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command) - N/A for internal upgrade
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
# Existing fetchit project structure (will be modified)
cmd/
└── fetchit/
    └── main.go           # CLI entry point - may need Podman v5 client initialization updates

pkg/
└── engine/
    ├── ansible.go        # Ansible playbook execution - review for Podman API changes
    ├── apply.go          # Apply configurations - review for Podman API changes
    ├── clean.go          # Cleanup operations - review for Podman API changes
    ├── common.go         # Common utilities - review for Podman API changes
    ├── config.go         # Configuration management - likely minimal changes
    ├── container.go      # Container operations - CRITICAL - will have Podman v5 API changes
    ├── disconnected.go   # Disconnected mode - review for Podman API changes
    ├── fetchit.go        # Main engine - review for Podman API changes
    ├── filetransfer.go   # File transfer - review for Podman API changes
    ├── gitauth.go        # Git authentication - likely minimal changes
    ├── image.go          # Image operations - review for Podman API changes
    ├── kube.go           # Kubernetes YAML - review for Podman API changes
    ├── raw.go            # Raw container deployment - CRITICAL - Podman v5 API changes
    ├── start.go          # Start operations - review for Podman API changes
    ├── systemd.go        # Systemd integration - review for Podman API changes
    ├── types.go          # Type definitions - review for Podman v5 type changes
    └── utils/
        ├── errors.go     # Error handling
        └── util.go       # Utilities

tests/                    # NEW - Unit tests to be added
├── unit/
│   ├── container_test.go    # Container operations unit tests
│   ├── filetransfer_test.go # File transfer unit tests
│   ├── systemd_test.go      # Systemd integration unit tests
│   ├── kube_test.go         # Kubernetes YAML unit tests
│   └── image_test.go        # Image operations unit tests
└── integration/              # Existing - GitHub Actions based
    └── (defined in .github/workflows/docker-image.yml)

.github/
└── workflows/
    └── docker-image.yml  # UPDATE - Change PODMAN_VER from v4.9.4 to v5.x

go.mod                    # UPDATE - Podman v5 dependencies
go.sum                    # REGENERATE - Checksums for new dependencies
```

**Structure Decision**: Using existing single-project structure with pkg/engine/ for business logic and cmd/fetchit/ for CLI entry point. Adding tests/ directory for new unit tests to complement existing GitHub Actions integration tests. This maintains fetchit's established architecture while adding missing unit test coverage.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

N/A - No constitution violations. This is a dependency upgrade following established project patterns.

## Phase 0: Research & API Change Analysis

### Research Tasks

1. **Podman v5 Breaking Changes Analysis**
   - Research: Identify all breaking API changes between Podman v4.2.0 and Podman v5.x
   - Source: Podman v5 release notes, migration guides, changelog
   - Output: List of affected APIs used by fetchit

2. **Go Version Requirements**
   - Research: Determine minimum Go version required by Podman v5
   - Source: Podman v5 go.mod file
   - Output: Go toolchain upgrade requirements

3. **Dependency Compatibility Matrix**
   - Research: Identify compatible versions of containers/common, containers/image, containers/storage for Podman v5
   - Source: Podman v5 go.mod dependencies
   - Output: Specific version numbers for all Podman-related dependencies

4. **Podman v5 Client Initialization**
   - Research: Changes to Podman client/connection initialization in v5
   - Source: Podman v5 bindings documentation and examples
   - Output: Code patterns for client initialization

5. **Container Spec Generation Changes**
   - Research: Changes to specgen.SpecGenerator and container creation APIs
   - Source: Podman v5 pkg/specgen documentation
   - Output: Migration patterns for container spec generation

6. **Image Operations API Changes**
   - Research: Changes to image pull, push, load operations
   - Source: Podman v5 pkg/bindings/images documentation
   - Output: Updated image operation patterns

7. **Testing Framework Compatibility**
   - Research: GitHub Actions runner compatibility with Podman v5 builds
   - Source: Podman v5 build requirements, GitHub Actions Ubuntu images
   - Output: Updated build dependencies list

**Expected research.md sections:**
- Podman v5 API Breaking Changes Summary
- Go Toolchain Upgrade Path
- Dependency Version Matrix
- Code Migration Patterns by Component
- Testing Infrastructure Updates

## Phase 1: Design & Contracts

### Data Model

**Minimal changes expected** - fetchit uses Podman's data models, not custom models. Will document:
- Changes to Podman types used in fetchit (if any)
- Updated specgen.SpecGenerator field requirements
- Changes to container status enums or constants

### API Contracts

**N/A** - This is an internal dependency upgrade. Fetchit's external interface (configuration files) remains unchanged. Internal Podman API usage will be updated based on v5 requirements.

### Quickstart

Will document:
1. Development environment setup with Go 1.21+ and Podman v5
2. Running upgraded tests locally
3. Common Podman v5 API patterns used in fetchit
4. Migration checklist for each component

## Phase 2: Implementation Tasks

**Note**: Detailed tasks will be generated by `/speckit.tasks` command. High-level task categories:

### Task Category 1: Dependency Upgrades
- Update go.mod Go version to 1.21+
- Update Podman dependencies to v5.x
- Update containers/common, containers/image, containers/storage
- Run go mod tidy and resolve conflicts

### Task Category 2: Code Adaptation
- Update container.go for Podman v5 API changes
- Update raw.go for spec generation changes
- Update image.go for image operation changes
- Update kube.go for pod API changes
- Update systemd.go for any Podman changes
- Update all other pkg/engine/ files as needed
- Update cmd/fetchit/main.go for client initialization

### Task Category 3: Testing Infrastructure
- Update GitHub Actions workflow PODMAN_VER to v5.x
- Verify all build dependencies for Podman v5
- Add unit tests for container operations
- Add unit tests for file transfer
- Add unit tests for systemd integration
- Add unit tests for Kubernetes YAML processing

### Task Category 4: Validation & Documentation
- Run all existing GitHub Actions tests
- Perform manual functional testing
- Update code comments for API changes
- Document breaking changes in PR descriptions

## Success Validation

Before marking complete, verify:
- [ ] All go.mod dependencies show v5.x for Podman packages
- [ ] Project builds without errors with Go 1.21+
- [ ] All existing GitHub Actions tests pass (100% pass rate)
- [ ] New unit tests added and passing
- [ ] Manual functional testing completed for all deployment mechanisms
- [ ] Zero regressions in existing functionality
- [ ] PR created with comprehensive change documentation

## Risk Mitigation Strategy

### Risk 1: Extensive Podman v5 Breaking Changes
**Mitigation**:
- Complete Phase 0 research before any code changes
- Identify all breaking changes upfront
- Create focused PRs per component rather than one massive change

### Risk 2: Go Toolchain Upgrade Issues
**Mitigation**:
- Verify Go 1.21+ compatibility with all dependencies
- Test build locally before CI
- Update GitHub Actions Go version in lockstep with code

### Risk 3: GitHub Actions Test Failures
**Mitigation**:
- Cache Podman v5 binary to reduce build time
- Run tests locally against Podman v5 before pushing
- Add detailed logging for failing tests

### Risk 4: Undocumented API Changes
**Mitigation**:
- Review Podman v5 source code examples
- Compare v4 and v5 API signatures directly
- Engage with Podman community if needed

## Next Steps

After this plan is approved:
1. Run `/speckit.tasks` to generate detailed implementation tasks from this plan
2. Begin Phase 0 research on Podman v5 breaking changes
3. Create research.md with findings
4. Proceed with incremental implementation following task order
