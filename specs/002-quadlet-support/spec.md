# Feature Specification: Quadlet Container Deployment

**Feature Branch**: `002-quadlet-support`
**Created**: 2025-12-30
**Status**: Draft
**Input**: User description: "I would like to enable quadlet support, add tests to deploy via quadlet, add examples to use quadlet, add github actions to test quadlet. I would also like to remove the way we launch containers with systemd today and replace that to use quadlet rather than a systemd command launching a specific container. You are allowed to research and gain all the knowledge you need to understand quadlet"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Declarative Container Management via Quadlet (Priority: P1)

As a fetchit user, I want to deploy containers using Quadlet's declarative `.container` files instead of systemd service files so that I can leverage Podman's built-in systemd integration for simpler, more maintainable container deployments.

**Why this priority**: This is the core functionality that replaces the current systemd deployment method. Quadlet provides a cleaner, Podman-native approach to systemd integration that eliminates the need for a separate systemd container to execute systemctl commands. This is the foundational change that all other stories build upon.

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

### User Story 3 - Multi-Resource Quadlet Support (Priority: P2)

As a fetchit user, I want to deploy Quadlet files for volumes, networks, and Kubernetes resources so that I can manage complete container environments declaratively.

**Why this priority**: While `.container` files are the primary use case, full Quadlet support includes volumes, networks, and Kubernetes YAML deployments. Supporting these enables complete infrastructure-as-code patterns.

**Independent Test**: Can be fully tested by deploying `.volume`, `.network`, and `.kube` files and verifying they create the corresponding Podman resources that containers can reference.

**Acceptance Scenarios**:

1. **Given** a repository containing `.volume` files, **When** fetchit processes the repository, **Then** Podman creates named volumes that containers can mount
2. **Given** a repository containing `.network` files, **When** fetchit processes the repository, **Then** Podman creates networks that containers can join
3. **Given** a repository containing `.kube` files with Kubernetes YAML, **When** fetchit processes the repository, **Then** Podman deploys the Kubernetes resources as pods
4. **Given** multiple related Quadlet files (network, volume, container), **When** fetchit deploys them, **Then** systemd dependencies ensure proper startup order

---

### User Story 4 - Validated Quadlet Examples (Priority: P2)

As a new fetchit user, I want example Quadlet configurations in the repository so that I can quickly understand how to use Quadlet with fetchit.

**Why this priority**: Documentation and examples are critical for user adoption. This story ensures users can easily get started with the new Quadlet functionality.

**Independent Test**: Can be fully tested by running the provided example configurations and verifying they deploy successfully.

**Acceptance Scenarios**:

1. **Given** the fetchit repository, **When** a user navigates to the examples directory, **Then** they find working `.container`, `.volume`, `.network`, and `.kube` examples with clear documentation
2. **Given** example Quadlet configurations, **When** a user follows the README instructions, **Then** they can deploy containers using Quadlet within 10 minutes
3. **Given** examples for both rootful and rootless scenarios, **When** a user tests them, **Then** both scenarios work without modification

---

### User Story 5 - Automated Quadlet Testing in CI (Priority: P2)

As a fetchit contributor, I want GitHub Actions workflows that test Quadlet functionality so that we can ensure Quadlet support remains stable across releases.

**Why this priority**: Automated testing prevents regressions and ensures the Quadlet method works reliably across different environments and Podman versions.

**Independent Test**: Can be fully tested by running the GitHub Actions workflow and verifying all Quadlet-related tests pass.

**Acceptance Scenarios**:

1. **Given** a pull request with Quadlet changes, **When** GitHub Actions runs, **Then** tests verify `.container` files deploy successfully
2. **Given** CI test workflows, **When** executed, **Then** they test both rootful and rootless Quadlet deployments
3. **Given** CI test workflows, **When** executed, **Then** they verify `.volume`, `.network`, and `.kube` file support
4. **Given** a test that deploys a container via Quadlet, **When** the test runs, **Then** it confirms the container is running and responds correctly

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
- What happens when a `.container` file requires a volume or network that hasn't been created yet? Quadlet's dependency system should handle ordering, but fetchit should validate that referenced `.volume` and `.network` files exist in the repository.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST support deploying containers using Podman Quadlet `.container` files
- **FR-002**: System MUST place Quadlet files in the correct systemd directory based on rootful (`/etc/containers/systemd/`) or rootless (`~/.config/containers/systemd/`) configuration
- **FR-003**: System MUST trigger systemd daemon-reload after placing or updating Quadlet files
- **FR-004**: System MUST support `.volume` files for creating named Podman volumes
- **FR-005**: System MUST support `.network` files for creating Podman networks
- **FR-006**: System MUST support `.kube` files for deploying Kubernetes YAML manifests
- **FR-007**: System MUST detect changes to Quadlet files in monitored Git repositories and update deployments accordingly
- **FR-008**: System MUST handle deletion of Quadlet files by removing corresponding files from systemd directories and stopping services
- **FR-009**: System MUST support both rootful and rootless Quadlet deployments based on configuration
- **FR-010**: System MUST provide examples for all supported Quadlet file types (`.container`, `.volume`, `.network`, `.kube`)
- **FR-011**: System MUST include GitHub Actions workflows that test Quadlet functionality
- **FR-012**: System MUST maintain backward compatibility with the existing systemd method during a deprecation period
- **FR-013**: System MUST log clear error messages when Quadlet files fail to deploy
- **FR-014**: System MUST create systemd Quadlet directories if they don't exist
- **FR-015**: System MUST validate that the Podman version supports Quadlet (Podman 4.4+)

### Key Entities

- **Quadlet File**: A declarative configuration file (`.container`, `.volume`, `.network`, `.kube`) that Podman's systemd generator converts into systemd units
- **Container Definition**: Specifications in a `.container` file including image, volumes, networks, environment variables, and resource limits
- **Volume Definition**: Named volume configuration in a `.volume` file
- **Network Definition**: Podman network configuration in a `.network` file
- **Kubernetes Deployment**: Kubernetes YAML manifest in a `.kube` file for pod-based deployments

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can deploy a container using a `.container` file without requiring the fetchit-systemd helper container
- **SC-002**: Quadlet deployments complete within the same time frame as legacy systemd deployments (within 10% variance)
- **SC-003**: All Quadlet file types (`.container`, `.volume`, `.network`, `.kube`) are tested by automated GitHub Actions workflows
- **SC-004**: Example configurations exist for at least 3 common deployment scenarios (simple container, container with volume, multi-container with network)
- **SC-005**: Migration documentation enables users to convert existing systemd deployments to Quadlet in under 15 minutes
- **SC-006**: Fetchit logs provide actionable error messages for Quadlet deployment failures within 5 seconds of detection
- **SC-007**: System supports both rootful and rootless Quadlet deployments configurable via fetchit configuration file
- **SC-008**: Fetchit processes Quadlet file changes (create, update, delete) from Git repositories within the same polling interval as other methods

## Scope & Boundaries

### In Scope

- Adding a new "quadlet" method to fetchit's deployment engine
- Support for `.container`, `.volume`, `.network`, and `.kube` file types
- Examples and documentation for common Quadlet use cases
- GitHub Actions test coverage for Quadlet functionality
- Migration guide from legacy systemd method to Quadlet
- Error handling and logging for Quadlet-specific failures

### Out of Scope

- Removing the legacy systemd method (deprecated but functional)
- Automatic conversion tools from systemd service files to Quadlet files
- Support for Quadlet `.build` and `.image` file types (can be added in future iterations)
- Modifications to Podman's Quadlet implementation itself
- Support for Podman versions earlier than 4.4

## Assumptions

- Podman version 4.4 or later is installed (Quadlet was integrated in 4.4)
- systemd is the init system on the host
- Users have appropriate permissions for rootful or rootless container operations
- Git repositories are accessible via fetchit's existing authentication methods
- The systemd Quadlet directories follow Podman's standard conventions

## Dependencies

- Podman v4.4+ with integrated Quadlet support
- systemd for service management
- Existing fetchit Git repository monitoring functionality
- Existing fetchit file change detection logic
