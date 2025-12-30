# Implementation Plan: Quadlet Container Deployment

**Branch**: `002-quadlet-support` | **Date**: 2025-12-30 | **Spec**: [spec.md](./spec.md)
**Input**: Feature specification from `/specs/002-quadlet-support/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Enable Podman Quadlet support in fetchit to replace the current systemd method with a declarative, Podman-native approach to container deployment. This eliminates the need for the fetchit-systemd helper container by leveraging Quadlet's built-in systemd integration to manage containers via `.container`, `.volume`, `.network`, and `.kube` files stored in Git repositories.

The implementation adds a new "quadlet" deployment method that monitors Git repositories for Quadlet files, places them in appropriate systemd directories, and triggers Podman's systemd generator to create and manage services. This approach simplifies deployments, reduces dependencies, and provides a more maintainable container management solution.

## Technical Context

**Language/Version**: Go 1.24.2 (as per go.mod)
**Primary Dependencies**:
- Podman v5.7.0 (containers/podman/v5)
- Existing fetchit Git monitoring (go-git/go-git/v5)
- systemd integration (native OS)
- Existing engine framework (pkg/engine)

**Storage**: File-based (Quadlet files in systemd directories, no database)
**Testing**: Go testing (tests/unit), GitHub Actions CI (existing workflows)
**Target Platform**: Linux with systemd (rootful and rootless)
**Project Type**: Single Go project with CLI
**Performance Goals**: Process Quadlet file changes within same polling interval as other methods (~1-2 seconds)
**Constraints**:
- Must maintain backward compatibility with existing systemd method
- Must support Podman 4.4+ (Quadlet integration point)
- File placement permissions for rootful (/etc/containers/systemd/) and rootless (~/.config/containers/systemd/)
- systemd daemon-reload coordination

**Scale/Scope**:
- Support 4 Quadlet file types (.container, .volume, .network, .kube)
- Multiple concurrent repository monitors
- Both rootful and rootless deployments

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**Status**: ✅ PASS - No constitution file exists with active principles. Project follows Go best practices and existing fetchit patterns.

**Observations**:
- Constitution template is empty (no ratified principles)
- Following existing fetchit architecture patterns from pkg/engine
- Maintaining consistency with current deployment methods (raw, kube, systemd, filetransfer, ansible)
- Standard Go project structure and testing practices apply

**Re-evaluation after Phase 1**: Will verify design aligns with existing engine architecture and doesn't introduce unnecessary complexity.

## Project Structure

### Documentation (this feature)

```text
specs/002-quadlet-support/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
├── checklists/          # Quality validation
│   └── requirements.md
└── spec.md              # Feature specification
```

### Source Code (repository root)

```text
pkg/engine/
├── quadlet.go           # New: Quadlet method implementation
├── types.go             # Modified: Add Quadlet type definition
├── fetchit.go           # Modified: Register quadlet method
├── systemd.go           # Existing: Legacy systemd method (deprecated but functional)
└── utils/               # Shared utilities

tests/unit/
└── quadlet_test.go      # New: Unit tests for Quadlet method

examples/
├── quadlet/             # New: Quadlet examples directory
│   ├── README.md        # Documentation for Quadlet examples
│   ├── simple.container # Example: Basic container
│   ├── httpd.container  # Example: Web server
│   ├── httpd.volume     # Example: Named volume
│   ├── httpd.network    # Example: Network definition
│   └── colors.kube      # Example: Kubernetes pod
├── quadlet-config.yaml  # New: fetchit config for Quadlet
└── quadlet-rootless.yaml # New: fetchit config for rootless Quadlet

.github/workflows/
└── docker-image.yml     # Modified: Add Quadlet test jobs

docs/
├── methods.rst          # Modified: Add Quadlet documentation
└── quadlet-migration.md # New: Migration guide from systemd to Quadlet
```

**Structure Decision**: Following existing fetchit patterns where each deployment method (raw, kube, systemd, filetransfer, ansible) is implemented as a struct in `pkg/engine/` with a corresponding `GetKind()`, `Process()`, `MethodEngine()`, and `Apply()` interface. Quadlet will be implemented as a new `Quadlet` struct following this established pattern.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

No violations - constitution is not yet ratified. Project follows standard Go conventions and existing fetchit architecture patterns.

---

## Phase 0: Research & Decisions

### Research Tasks

1. **Quadlet File Format Specifications**
   - Research: Detailed syntax for .container, .volume, .network, .kube files
   - Research: Podman documentation for Quadlet unit file options
   - Research: systemd dependencies between Quadlet units

2. **Directory and Permission Requirements**
   - Research: Exact directory paths for rootful vs rootless
   - Research: Permission requirements for file placement
   - Research: XDG_RUNTIME_DIR handling for rootless

3. **systemd Integration Patterns**
   - Research: When and how to trigger daemon-reload
   - Research: Service naming conventions from Quadlet files
   - Research: Error detection from Podman's systemd generator

4. **File Change Detection**
   - Research: Existing fetchit patterns for file monitoring
   - Research: Handling create/update/delete operations
   - Research: Glob patterns for Quadlet file types

5. **Testing Strategy**
   - Research: Existing GitHub Actions patterns in docker-image.yml
   - Research: Test scenarios from other methods (systemd-validate, etc.)
   - Research: Rootless testing in CI environment

### Expected Outputs

- `research.md` documenting:
  - Quadlet file format specifications with examples
  - Directory structure and permission requirements
  - systemd daemon-reload best practices
  - File monitoring integration points
  - CI testing approach

---

## Phase 1: Design

### Data Model (`data-model.md`)

**Quadlet Deployment Method**
- Configuration: Root (bool), Enable (bool), Restart (bool)
- Target: Git repository URL, branch, path, glob pattern
- File Types: .container, .volume, .network, .kube
- State: Initial run flag, deployment directory path

**Quadlet File Metadata**
- Source path: Location in Git repository
- Target path: Destination in systemd directory
- File type: Enum (container, volume, network, kube)
- Service name: Derived from filename
- Change type: Create, Update, Delete

**Directory Mappings**
- Rootful: /etc/containers/systemd/
- Rootless: ~/.config/containers/systemd/ (or $XDG_CONFIG_HOME)

### API Contracts (`contracts/`)

**Engine Interface** (existing pattern to follow)
```go
// Quadlet implements the Method interface
type Quadlet struct {
    CommonMethod `mapstructure:",squash"`
    Root         bool `mapstructure:"root"`
    Enable       bool `mapstructure:"enable"`
    Restart      bool `mapstructure:"restart"`
}

func (q *Quadlet) GetKind() string
func (q *Quadlet) Process(ctx, conn context.Context, skew int)
func (q *Quadlet) MethodEngine(ctx context.Context, conn context.Context, change *object.Change, path string) error
func (q *Quadlet) Apply(ctx, conn context.Context, currentState, desiredState plumbing.Hash, tags *[]string) error
```

**Configuration Schema** (YAML)
```yaml
targets:
  - name: quadlet-httpd
    url: https://github.com/user/containers.git
    branch: main
    targetPath: quadlet/
    schedule: "*/5 * * * *"
    method:
      type: quadlet
      root: true
      enable: true
      restart: false
```

### Quickstart Guide (`quickstart.md`)

Will include:
1. Prerequisites check (Podman 4.4+, systemd)
2. Creating a simple .container file
3. Configuring fetchit to monitor Quadlet files
4. Verifying deployment and service status
5. Migration steps from existing systemd method

---

## Phase 2: Task Generation

**Note**: Task generation happens via `/speckit.tasks` command, not during `/speckit.plan`.

The tasks will be organized as:
- Foundation: Quadlet struct and interface implementation
- Core: File placement, daemon-reload, service management
- File Types: Support for .container, .volume, .network, .kube
- Testing: Unit tests, integration tests, CI workflows
- Documentation: Examples, migration guide, method documentation
- Cleanup: Deprecation warnings for legacy systemd method

---

## Implementation Notes

### Key Design Decisions

1. **No Helper Container**: Unlike the legacy systemd method which uses `quay.io/fetchit/fetchit-systemd` container to execute systemctl commands, Quadlet relies entirely on Podman's native systemd generator. Files are simply placed in the correct directory and daemon-reload is triggered.

2. **Follow Existing Patterns**: The Quadlet implementation will mirror the structure of existing methods (particularly systemd and filetransfer) to maintain consistency in the codebase.

3. **Backward Compatibility**: The legacy systemd method remains functional but deprecated. Both can coexist during the transition period.

4. **Directory Creation**: Implementation must create systemd Quadlet directories if they don't exist, with appropriate permissions.

5. **Error Handling**: Quadlet file syntax errors will be detected via systemd service failures, which must be logged clearly by fetchit.

### Integration Points

- **Git Monitoring**: Leverage existing `applyChanges()` and `runChanges()` from engine framework
- **File Transfer**: Reuse file copy logic similar to FileTransfer method
- **systemd Interaction**: Use Podman API or exec systemctl daemon-reload (investigate in Phase 0)
- **Logging**: Use existing logger infrastructure with Quadlet-specific context

### Success Criteria Mapping

- SC-001: Achieved by file placement + daemon-reload (no helper container)
- SC-002: Performance tracked via existing fetchit timing logs
- SC-003: CI workflows test all file types
- SC-004: Examples directory with 3+ scenarios
- SC-005: Migration guide in quickstart.md
- SC-006: Error logging within Process() and MethodEngine()
- SC-007: Root bool in Quadlet config
- SC-008: Same polling interval as other methods (uses existing engine)
