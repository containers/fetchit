# Feature Specification: Quadlet Container Deployment

**Feature Branch**: `002-quadlet-support`
**Created**: 2025-12-30
**Updated**: 2026-01-06
**Status**: Draft
**Input**: User description: "I want to allow my fetchit project to use quadlet. Quadlet should deploy using the file transfer under pkg/engine and systemd to start the service"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Declarative Container Management via Quadlet (Priority: P1)

As a fetchit user, I want to deploy containers using Quadlet's declarative `.container` files instead of systemd service files so that I can leverage Podman's built-in systemd integration for simpler, more maintainable container deployments.

**Why this priority**: This is the core functionality that replaces the current systemd deployment method. Quadlet provides a cleaner, Podman-native approach to systemd integration. The implementation uses the existing file transfer mechanism from `pkg/engine/filetransfer.go` to copy Quadlet files to the appropriate systemd directories, and then uses systemd to start the services. This is the foundational change that all other stories build upon.

**Independent Test**: Can be fully tested by configuring fetchit to monitor a repository containing `.container` files and verifying that Podman creates corresponding systemd services and starts containers without requiring the fetchit-systemd helper container.

**Acceptance Scenarios**:

1. **Given** a Git repository containing a `.container` file, **When** fetchit processes the repository with the Quadlet method configured, **Then** the `.container` file is placed in the appropriate systemd Quadlet directory and a corresponding systemd service is generated and started by Podman
2. **Given** a `.container` file that specifies an image, volumes, and network configuration, **When** Podman processes the Quadlet file, **Then** the container starts with all specified configurations applied
3. **Given** an existing container managed by the legacy systemd method, **When** the deployment is migrated to use Quadlet, **Then** the container is managed declaratively without requiring the fetchit-systemd helper container

---

### User Story 2 - Repository-Driven Quadlet Deployment (Priority: P1)

As a fetchit user, I want fetchit to automatically deploy Quadlet files from my Git repository so that container deployments stay in sync with my version-controlled infrastructure configuration.

**Why this priority**: This ensures Quadlet files benefit from fetchit's core git-based workflow, maintaining the same automatic deployment capabilities users expect from other fetchit methods.

**Independent Test**: Can be fully tested by pushing a `.container` file to a monitored repository and verifying that fetchit detects the change, places the file in the correct location, triggers systemd daemon-reload, and starts the service.

**Acceptance Scenarios**:

1. **Given** fetchit is monitoring a Git repository for Quadlet files, **When** a new `.container` file is committed, **Then** fetchit copies the file to the systemd Quadlet directory and the container starts automatically
2. **Given** an existing `.container` file in a monitored repository, **When** the file is modified, **Then** fetchit updates the file on the host and systemd restarts the service with the new configuration
3. **Given** a `.container` file in a monitored repository, **When** the file is deleted from the repository, **Then** fetchit removes the file from the systemd Quadlet directory and the associated service stops
4. **Given** fetchit configured for both rootful and rootless Quadlet deployments, **When** files are committed, **Then** rootful files go to `/etc/containers/systemd/` and rootless files go to `~/.config/containers/systemd/`

---

### User Story 3 - Comprehensive Multi-Resource Quadlet Support (Priority: P2)

As a fetchit user, I want to deploy all Quadlet file types supported by Podman v5.7.0 (volumes, networks, pods, images, builds, artifacts, and Kubernetes resources) so that I can manage complete container environments declaratively.

**Why this priority**: While `.container` files are the primary use case, Podman v5.7.0 supports a complete ecosystem of Quadlet file types: `.volume`, `.network`, `.pod`, `.image`, `.build`, `.artifact`, and `.kube`. Supporting all these types enables comprehensive infrastructure-as-code patterns including image building, artifact management, and multi-container pod deployments.

**Independent Test**: Can be fully tested by deploying all supported Quadlet file types and verifying they create the corresponding Podman resources with proper systemd dependency ordering.

**Acceptance Scenarios**:

1. **Given** a repository containing `.volume` files, **When** fetchit processes the repository, **Then** Podman creates named volumes that containers can mount
2. **Given** a repository containing `.network` files, **When** fetchit processes the repository, **Then** Podman creates networks that containers can join
3. **Given** a repository containing `.kube` files with Kubernetes YAML (including support for multiple YAML files), **When** fetchit processes the repository, **Then** Podman deploys the Kubernetes resources as pods
4. **Given** a repository containing `.pod` files with StopTimeout configuration, **When** fetchit processes the repository, **Then** Podman creates multi-container pods with proper timeout settings
5. **Given** a repository containing `.build` files with BuildArg and IgnoreFile options, **When** fetchit processes the repository, **Then** Podman builds container images with specified build arguments and ignore patterns
6. **Given** a repository containing `.image` files, **When** fetchit processes the repository, **Then** Podman pulls container images from registries
7. **Given** a repository containing `.artifact` files (new in v5.7.0), **When** fetchit processes the repository, **Then** Podman manages OCI artifacts
8. **Given** multiple related Quadlet files (network, volume, container, pod), **When** fetchit deploys them, **Then** systemd dependencies with templated support ensure proper startup order

---

### User Story 4 - Validated Quadlet Examples (Priority: P2)

As a new fetchit user, I want example Quadlet configurations for all supported file types in the repository so that I can quickly understand how to use Quadlet with fetchit.

**Why this priority**: Documentation and examples are critical for user adoption. This story ensures users can easily get started with the full range of Quadlet v5.7.0 functionality.

**Independent Test**: Can be fully tested by running the provided example configurations and verifying they deploy successfully.

**Acceptance Scenarios**:

1. **Given** the fetchit repository, **When** a user navigates to the examples directory, **Then** they find working examples for all Quadlet file types: `.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, and `.kube` with clear documentation
2. **Given** example Quadlet configurations, **When** a user follows the README instructions, **Then** they can deploy containers using Quadlet within 10 minutes
3. **Given** examples for both rootful and rootless scenarios, **When** a user tests them, **Then** both scenarios work without modification
4. **Given** examples demonstrating v5.7.0 features (HttpProxy, StopTimeout, BuildArg, IgnoreFile), **When** a user tests them, **Then** all new features work as documented

---

### User Story 5 - Automated Quadlet Testing in CI (Priority: P2)

As a fetchit contributor, I want GitHub Actions workflows that test all Quadlet v5.7.0 functionality so that we can ensure Quadlet support remains stable across releases.

**Why this priority**: Automated testing prevents regressions and ensures the Quadlet method works reliably across different environments and Podman versions, including all v5.7.0 file types and features.

**Independent Test**: Can be fully tested by running the GitHub Actions workflow and verifying all Quadlet-related tests pass.

**Acceptance Scenarios**:

1. **Given** a pull request with Quadlet changes, **When** GitHub Actions runs, **Then** tests verify all Quadlet file types (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`) deploy successfully
2. **Given** CI test workflows, **When** executed, **Then** they test both rootful and rootless Quadlet deployments
3. **Given** CI test workflows, **When** executed, **Then** they verify v5.7.0-specific features including HttpProxy, StopTimeout, BuildArg, IgnoreFile, and OCI artifact management
4. **Given** a test that deploys a container via Quadlet, **When** the test runs, **Then** it confirms the container is running and responds correctly
5. **Given** tests that verify `.build` files, **When** the tests run, **Then** they confirm images are built successfully with proper build arguments
6. **Given** tests that verify `.pod` files, **When** the tests run, **Then** they confirm multi-container pods are created with proper timeout configurations

---

### User Story 6 - Legacy Systemd Method Deprecation (Priority: P3)

As a fetchit maintainer, I want clear migration documentation from the legacy systemd method to Quadlet so that users can transition smoothly while maintaining backward compatibility during a deprecation period.

**Why this priority**: While important for the long-term health of the codebase, this is lower priority than getting Quadlet working. Existing deployments should continue to work while users migrate at their own pace.

**Independent Test**: Can be fully tested by verifying the legacy systemd method still works (deprecated but functional) and that migration documentation successfully guides users to Quadlet.

**Acceptance Scenarios**:

1. **Given** a user currently using the systemd method, **When** they upgrade to a version with Quadlet support, **Then** their existing deployments continue to work unchanged
2. **Given** migration documentation, **When** a user follows it, **Then** they can convert their systemd service files to Quadlet `.container` files
3. **Given** both methods enabled in a fetchit configuration, **When** fetchit runs, **Then** both methods process their respective files without conflict
4. **Given** deprecation warnings in the logs, **When** a user uses the legacy systemd method, **Then** they receive clear guidance on migrating to Quadlet

---

### Edge Cases

- What happens when a `.container` file references a non-existent image? The Quadlet service should fail to start with a clear error from Podman, and fetchit logs should reflect the failure.
- How does the system handle `.container` files with invalid syntax? Podman's systemd generator should reject the file, preventing service creation, and fetchit should log the validation error.
- What happens when both legacy systemd and Quadlet files exist for the same service name? The configuration should document this as unsupported, and fetchit should warn about naming conflicts.
- How does the system handle Quadlet files during rapid repository updates? fetchit should process changes sequentially and systemd should handle daemon-reload operations safely.
- What happens when systemd Quadlet directories don't exist? fetchit should create the required directories with appropriate permissions before copying files.
- How does the system handle rootless Quadlet when `XDG_RUNTIME_DIR` is not set? fetchit should detect this condition and either set a sensible default or fail with a clear error message.
- What happens when a `.container` file requires a volume or network that hasn't been created yet? Quadlet's dependency system with templated support should handle ordering, but fetchit should validate that referenced `.volume` and `.network` files exist in the repository.
- How does the system handle `.build` files when build context or Dockerfile is missing? The build should fail with a clear error message indicating the missing files.
- What happens when a `.pod` file references containers that don't exist? Podman should fail to create the pod and fetchit should log the dependency error.
- How does the system handle `.image` files when the registry is unreachable? The image pull should fail with a clear network error and fetchit should log the failure.
- What happens when an `.artifact` file references an OCI artifact that doesn't exist? The artifact fetch should fail with a clear error from Podman.
- How does the system handle `.kube` files with multiple YAML documents? Podman v5.7.0 supports multiple YAML files, and fetchit should deploy all resources defined in the YAML documents.
- What happens when HttpProxy is set to false in a `.container` file but the container needs proxy access? The container will not have proxy environment variables and may fail to access external resources; this should be documented in examples.

## Requirements *(mandatory)*

### Functional Requirements

#### Core Quadlet File Type Support (Podman v5.7.0)

- **FR-001**: System MUST support deploying containers using Podman Quadlet `.container` files
- **FR-002**: System MUST support `.volume` files for creating named Podman volumes
- **FR-003**: System MUST support `.network` files for creating Podman networks
- **FR-004**: System MUST support `.pod` files for creating multi-container Podman pods
- **FR-005**: System MUST support `.kube` files for deploying Kubernetes YAML manifests (including multiple YAML files per `.kube` file)
- **FR-006**: System MUST support `.build` files for building container images locally
- **FR-007**: System MUST support `.image` files for pulling container images from registries
- **FR-008**: System MUST support `.artifact` files for managing OCI artifacts (new in v5.7.0)

#### Podman v5.7.0 Configuration Options

- **FR-009**: System MUST support the `HttpProxy` key in `.container` files to control HTTP proxy forwarding
- **FR-010**: System MUST support the `StopTimeout` key in `.pod` files to configure pod stop timeout
- **FR-011**: System MUST support the `BuildArg` key in `.build` files to specify build arguments
- **FR-012**: System MUST support the `IgnoreFile` key in `.build` files to specify ignore files for builds

#### File Management and Deployment

- **FR-013**: System MUST place Quadlet files in the correct systemd directory based on rootful (`/etc/containers/systemd/`) or rootless (`~/.config/containers/systemd/`) configuration
- **FR-014**: System MUST trigger systemd daemon-reload after placing or updating Quadlet files
- **FR-015**: System MUST detect changes to Quadlet files in monitored Git repositories and update deployments accordingly
- **FR-016**: System MUST handle deletion of Quadlet files by removing corresponding files from systemd directories and stopping services
- **FR-017**: System MUST support both rootful and rootless Quadlet deployments based on configuration
- **FR-018**: System MUST create systemd Quadlet directories if they don't exist
- **FR-019**: System MUST handle templated dependencies for volumes and networks (v5.7.0 feature)

#### Testing, Documentation, and Compatibility

- **FR-020**: System MUST provide examples for all supported Quadlet file types (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`)
- **FR-021**: System MUST include examples demonstrating v5.7.0-specific features (HttpProxy, StopTimeout, BuildArg, IgnoreFile)
- **FR-022**: System MUST include GitHub Actions workflows that test all Quadlet file types and v5.7.0 features
- **FR-023**: System MUST maintain backward compatibility with the existing systemd method during a deprecation period
- **FR-024**: System MUST log clear error messages when Quadlet files fail to deploy
- **FR-025**: System MUST validate that the Podman version is v5.7.0 or later to support all features

#### Backward Compatibility and Non-Breaking Changes (CRITICAL)

- **FR-026**: System MAY modify `pkg/engine/systemd.go` and `pkg/engine/filetransfer.go` IF changes support Quadlet integration AND all existing GitHub Actions tests pass (systemd-validate, filetransfer-validate)
- **FR-027**: System MUST NOT modify Method implementations in `pkg/engine/kube.go`, `pkg/engine/ansible.go`, or `pkg/engine/raw.go` - these must remain unchanged
- **FR-028**: System MUST ensure existing Quadlet file types (`.container`, `.volume`, `.network`, `.kube`) deployed before this update continue to work without modification
- **FR-029**: System MUST NOT change the Method interface defined in `pkg/engine/types.go` - Quadlet must implement existing interface without modifications
- **FR-030**: System MUST verify all existing GitHub Actions tests continue to pass (systemd-validate, kube-validate, ansible-validate, filetransfer-validate, raw-validate)
- **FR-031**: System MUST provide rollback procedure documentation in case issues arise with Quadlet extension
- **FR-032**: System MUST NOT modify existing configuration schema - new Quadlet fields must be optional and additive only
- **FR-033**: System MUST maintain existing glob pattern behavior for all methods - Quadlet glob patterns must not interfere with other methods
- **FR-034**: System MUST ensure file transfer mechanism changes (if any) are backward compatible with all methods that use it (systemd, kube, ansible, filetransfer, raw)
- **FR-035**: System MUST validate that concurrent deployments using different methods (e.g., systemd + quadlet) do not conflict

### Key Entities

- **Quadlet File**: A declarative configuration file (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`) that Podman's systemd generator converts into systemd units
- **Container Definition**: Specifications in a `.container` file including image, volumes, networks, environment variables, resource limits, and HttpProxy settings
- **Pod Definition**: Multi-container pod configuration in a `.pod` file with StopTimeout and networking settings
- **Volume Definition**: Named volume configuration in a `.volume` file with support for templated dependencies
- **Network Definition**: Podman network configuration in a `.network` file with support for templated dependencies
- **Build Definition**: Container image build specification in a `.build` file including Dockerfile path, BuildArg values, and IgnoreFile patterns
- **Image Definition**: Container image pull specification in an `.image` file including registry, tag, and pull policy
- **Artifact Definition**: OCI artifact management specification in an `.artifact` file (new in v5.7.0)
- **Kubernetes Deployment**: Kubernetes YAML manifest(s) in a `.kube` file for pod-based deployments, with support for multiple YAML documents

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can deploy containers using all Podman v5.7.0 Quadlet file types (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`) without requiring the fetchit-systemd helper container
- **SC-002**: Quadlet deployments complete within the same time frame as legacy systemd deployments (within 10% variance)
- **SC-003**: All eight Quadlet file types supported by v5.7.0 are tested by automated GitHub Actions workflows
- **SC-004**: Example configurations exist for at least 8 deployment scenarios covering each Quadlet file type (container, volume, network, pod, build, image, artifact, kube)
- **SC-005**: Example configurations demonstrate all v5.7.0-specific features (HttpProxy, StopTimeout, BuildArg, IgnoreFile, OCI artifacts, multiple YAML files in .kube)
- **SC-006**: Migration documentation enables users to convert existing systemd deployments to Quadlet in under 15 minutes
- **SC-007**: Fetchit logs provide actionable error messages for Quadlet deployment failures within 5 seconds of detection
- **SC-008**: System supports both rootful and rootless Quadlet deployments configurable via fetchit configuration file
- **SC-009**: Fetchit processes Quadlet file changes (create, update, delete) from Git repositories within the same polling interval as other methods
- **SC-010**: Users can build container images using `.build` files with custom build arguments successfully
- **SC-011**: Multi-container pods deployed via `.pod` files start with proper container dependencies and timeout configurations
- **SC-012**: Templated dependencies for volumes and networks resolve correctly during systemd service startup

### Backward Compatibility Outcomes (CRITICAL)

- **SC-013**: All existing deployments using systemd, kube, ansible, filetransfer, or raw methods continue to function without any code changes after Quadlet extension
- **SC-014**: All existing GitHub Actions CI tests for other methods (systemd-validate, kube-validate, etc.) pass without modification
- **SC-015**: Existing Quadlet deployments (`.container`, `.volume`, `.network`, `.kube` files deployed before this update) continue to work without requiring any changes
- **SC-016**: Users can run multiple deployment methods concurrently (e.g., systemd + quadlet, kube + quadlet) without conflicts
- **SC-017**: Rollback from extended Quadlet to previous version completes successfully without data loss
- **SC-018**: No changes to the Method interface contract in `pkg/engine/types.go`

## Scope & Boundaries

### In Scope

- Adding a new "quadlet" method to fetchit's deployment engine
- Support for all Podman v5.7.0 Quadlet file types: `.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, and `.kube`
- Support for v5.7.0-specific configuration options: HttpProxy, StopTimeout, BuildArg, IgnoreFile
- Support for templated dependencies for volumes and networks
- Support for multiple YAML files in `.kube` files
- Examples and documentation for all Quadlet file types and v5.7.0 features
- GitHub Actions test coverage for all Quadlet file types and v5.7.0 functionality
- Migration guide from legacy systemd method to Quadlet
- Error handling and logging for Quadlet-specific failures
- Using the existing file transfer mechanism from `pkg/engine/filetransfer.go`

### Out of Scope

- Removing the legacy systemd method (deprecated but functional)
- Automatic conversion tools from systemd service files to Quadlet files
- Modifications to Podman's Quadlet implementation itself
- Support for Podman versions earlier than v5.7.0
- Advanced Quadlet features beyond v5.7.0 (future versions)
- Custom Quadlet generators or extensions to the Podman Quadlet system

## Assumptions

- Podman version 5.7.0 or later is installed to support all Quadlet file types and v5.7.0 features (`.artifact`, HttpProxy, StopTimeout, BuildArg, IgnoreFile, templated dependencies, multiple YAML support)
- systemd is the init system on the host
- Users have appropriate permissions for rootful or rootless container operations
- Git repositories are accessible via fetchit's existing authentication methods
- The systemd Quadlet directories follow Podman's standard conventions (`/etc/containers/systemd/` for rootful, `~/.config/containers/systemd/` for rootless)
- The existing file transfer mechanism in `pkg/engine/filetransfer.go` is used to copy Quadlet files from the Git repository to systemd directories
- systemd daemon-reload is triggered after Quadlet files are placed to generate service units
- For `.build` files, build context and Dockerfile are available in the Git repository or accessible to Podman
- For `.image` files, container registries are accessible from the host system
- For `.artifact` files, OCI artifact registries are accessible from the host system

## Dependencies

- Podman v5.7.0 with comprehensive Quadlet support including:
  - All eight Quadlet file types (`.container`, `.volume`, `.network`, `.pod`, `.build`, `.image`, `.artifact`, `.kube`)
  - v5.7.0-specific features (HttpProxy, StopTimeout, BuildArg, IgnoreFile, OCI artifacts, multiple YAML files, templated dependencies)
- systemd for service management and daemon-reload operations
- Existing fetchit Git repository monitoring functionality
- Existing fetchit file change detection logic
- Existing file transfer mechanism in `pkg/engine/filetransfer.go`
- Container image registries for `.image` file support
- OCI artifact registries for `.artifact` file support (new in v5.7.0)
