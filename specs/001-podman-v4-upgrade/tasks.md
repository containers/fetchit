# Tasks: Podman v5 Dependency Upgrade with Comprehensive Testing

**Input**: Design documents from `/specs/001-podman-v4-upgrade/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, quickstart.md

**Tests**: Unit tests and integration tests are explicitly requested in the feature specification (User Story 2 and User Story 3).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Using existing fetchit single-project structure:
- **Root**: Repository root `/Users/rcook/git/fetchit`
- **Source**: `pkg/engine/` for business logic
- **CLI**: `cmd/fetchit/` for entry point
- **Tests**: `tests/unit/` for new unit tests
- **Config**: `go.mod`, `go.sum` for dependencies
- **CI**: `.github/workflows/` for GitHub Actions

---

## Phase 1: Setup & Research

**Purpose**: Research Podman v5 breaking changes and prepare environment

- [X] T001 Research Podman v5 breaking changes by reviewing release notes at https://github.com/containers/podman/releases
- [X] T002 [P] Identify Podman v5 minimum Go version requirement by checking Podman v5 go.mod
- [X] T003 [P] Document Podman v5 dependency versions (containers/common, containers/image, containers/storage) in specs/001-podman-v4-upgrade/research.md
- [X] T004 [P] Analyze breaking API changes in github.com/containers/podman/v5/pkg/bindings/containers
- [X] T005 [P] Analyze breaking API changes in github.com/containers/podman/v5/pkg/specgen
- [X] T006 [P] Analyze breaking API changes in github.com/containers/podman/v5/pkg/bindings/images
- [X] T007 Document code migration patterns in specs/001-podman-v4-upgrade/research.md
- [X] T008 Create tests/unit/ directory structure for new unit tests

**Checkpoint**: ✓ Research complete - specific breaking changes identified and documented

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core dependency and Go version upgrades that MUST be complete before ANY code changes

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T009 Update Go version from 1.17 to 1.21+ in go.mod
- [X] T010 Update github.com/containers/podman/v4 to github.com/containers/podman/v5 in go.mod
- [X] T011 [P] Update github.com/containers/common to Podman v5 compatible version in go.mod
- [X] T012 [P] Update github.com/containers/image/v5 to Podman v5 compatible version in go.mod
- [X] T013 [P] Update github.com/containers/storage to Podman v5 compatible version in go.mod
- [X] T014 Run `go mod tidy` to resolve all dependency conflicts
- [ ] T015 Run `go mod vendor` to update vendored dependencies
- [ ] T016 Attempt initial build with `make build` and document compilation errors in specs/001-podman-v4-upgrade/research.md

**Checkpoint**: Foundation ready - dependencies upgraded, build errors documented

---

## Phase 3: User Story 1 - Dependency Upgrade (Priority: P1) 🎯 MVP

**Goal**: Successfully upgrade all Go dependencies to latest Podman v5 versions and fix breaking API changes in core container operations

**Independent Test**: Run `go mod tidy`, verify v5.x versions in go.mod, successfully build project with `make build`, and verify no compilation errors

### Implementation for User Story 1

- [ ] T017 [P] [US1] Fix Podman v5 API changes in pkg/engine/container.go (SpecGenerator, CreateWithSpec)
- [ ] T018 [P] [US1] Fix Podman v5 API changes in pkg/engine/raw.go (spec generation)
- [ ] T019 [P] [US1] Fix Podman v5 API changes in pkg/engine/image.go (image operations)
- [ ] T020 [P] [US1] Fix Podman v5 API changes in pkg/engine/types.go (type definitions)
- [ ] T021 [US1] Fix Podman v5 API changes in pkg/engine/common.go (shared utilities)
- [ ] T022 [US1] Fix Podman v5 client initialization in cmd/fetchit/main.go if needed
- [ ] T023 [US1] Update import paths from v4 to v5 in pkg/engine/kube.go
- [ ] T024 [US1] Update import paths from v4 to v5 in pkg/engine/systemd.go
- [ ] T025 [US1] Update import paths from v4 to v5 in pkg/engine/apply.go
- [ ] T026 [US1] Update import paths from v4 to v5 in pkg/engine/clean.go
- [ ] T027 [US1] Update import paths from v4 to v5 in pkg/engine/disconnected.go
- [ ] T028 [US1] Update import paths from v4 to v5 in pkg/engine/filetransfer.go
- [ ] T029 [US1] Update import paths from v4 to v5 in pkg/engine/ansible.go
- [ ] T030 [US1] Update import paths from v4 to v5 in pkg/engine/fetchit.go
- [ ] T031 [US1] Update import paths from v4 to v5 in pkg/engine/start.go
- [ ] T032 [US1] Run `make build` and verify successful compilation
- [ ] T033 [US1] Run existing unit test (pkg/engine/utils/errors_test.go) and verify it passes with v5
- [ ] T034 [US1] Verify go.mod shows all Podman packages at v5.x versions
- [ ] T035 [US1] Update code comments documenting v5 API changes in modified files

**Checkpoint**: User Story 1 complete - Project builds successfully with Podman v5, all imports updated, core API changes fixed

---

## Phase 4: User Story 2 - Enhanced Unit Test Coverage (Priority: P2)

**Goal**: Create comprehensive unit tests for all container management mechanisms

**Independent Test**: Run `go test ./...` and verify new tests cover container operations, file transfer, systemd integration, Kubernetes YAML, and error handling

### Unit Tests for User Story 2

- [ ] T036 [P] [US2] Create unit tests for container creation in tests/unit/container_test.go
- [ ] T037 [P] [US2] Create unit tests for container start/stop/remove in tests/unit/container_test.go
- [ ] T038 [P] [US2] Create unit tests for container capability management in tests/unit/container_test.go
- [ ] T039 [P] [US2] Create unit tests for spec generation in tests/unit/container_test.go
- [ ] T040 [P] [US2] Create unit tests for directory file transfer in tests/unit/filetransfer_test.go
- [ ] T041 [P] [US2] Create unit tests for single-file transfer in tests/unit/filetransfer_test.go
- [ ] T042 [P] [US2] Create unit tests for systemd unit deployment in tests/unit/systemd_test.go
- [ ] T043 [P] [US2] Create unit tests for systemd service enabling (system mode) in tests/unit/systemd_test.go
- [ ] T044 [P] [US2] Create unit tests for systemd service enabling (user mode) in tests/unit/systemd_test.go
- [ ] T045 [P] [US2] Create unit tests for systemd service restart in tests/unit/systemd_test.go
- [ ] T046 [P] [US2] Create unit tests for Kubernetes pod creation in tests/unit/kube_test.go
- [ ] T047 [P] [US2] Create unit tests for Kubernetes YAML processing in tests/unit/kube_test.go
- [ ] T048 [P] [US2] Create unit tests for image operations (pull/load) in tests/unit/image_test.go
- [ ] T049 [P] [US2] Create unit tests for ansible playbook deployment in tests/unit/ansible_test.go
- [ ] T050 [P] [US2] Create unit tests for error handling (missing files) in tests/unit/errors_test.go
- [ ] T051 [P] [US2] Create unit tests for error handling (invalid configurations) in tests/unit/errors_test.go
- [ ] T052 [P] [US2] Create unit tests for error handling (network failures) in tests/unit/errors_test.go
- [ ] T053 [P] [US2] Create unit tests for error handling (permission issues) in tests/unit/errors_test.go
- [ ] T054 [US2] Run `go test ./tests/unit/... -v` and verify all new tests pass
- [ ] T055 [US2] Run `go test ./... -cover -coverprofile=coverage.out` and verify 60%+ coverage for pkg/engine/
- [ ] T056 [US2] Verify unit tests complete in under 30 seconds

**Checkpoint**: User Story 2 complete - Comprehensive unit tests added, 60%+ coverage achieved, tests pass in <30 seconds

---

## Phase 5: User Story 3 - GitHub Actions Integration Test Enhancement (Priority: P1)

**Goal**: Update GitHub Actions workflows to build and test against Podman v5

**Independent Test**: Push to feature branch, observe GitHub Actions execution, verify all test jobs pass with Podman v5

### Implementation for User Story 3

- [ ] T057 [US3] Update PODMAN_VER from v4.9.4 to latest v5.x tag in .github/workflows/docker-image.yml
- [ ] T058 [US3] Rename build-podman-v4 job to build-podman-v5 in .github/workflows/docker-image.yml
- [ ] T059 [US3] Update Podman build steps for v5 compatibility in .github/workflows/docker-image.yml
- [ ] T060 [US3] Verify build dependencies (libsystemd-dev, libseccomp-dev, etc.) are sufficient for Podman v5
- [ ] T061 [US3] Update Go version in GitHub Actions to 1.21+ in .github/workflows/docker-image.yml
- [ ] T062 [US3] Update cache key for Podman v5 binary in .github/workflows/docker-image.yml
- [ ] T063 [US3] Test raw-validate job with Podman v5 by pushing to feature branch
- [ ] T064 [US3] Test kube-validate job with Podman v5 by pushing to feature branch
- [ ] T065 [US3] Test systemd-validate job with Podman v5 by pushing to feature branch
- [ ] T066 [US3] Test filetransfer-validate job with Podman v5 by pushing to feature branch
- [ ] T067 [US3] Test ansible-validate job with Podman v5 by pushing to feature branch
- [ ] T068 [US3] Test clean-validate job with Podman v5 by pushing to feature branch
- [ ] T069 [US3] Test disconnected-validate job with Podman v5 by pushing to feature branch
- [ ] T070 [US3] Verify all existing integration test jobs pass (100% pass rate)
- [ ] T071 [US3] Verify GitHub Actions tests complete within 45 minutes total
- [ ] T072 [US3] Verify test logs are captured for any failures

**Checkpoint**: User Story 3 complete - GitHub Actions updated for Podman v5, all integration tests passing

---

## Phase 6: User Story 4 - Functional Validation Testing (Priority: P2)

**Goal**: Perform manual functional testing of core mechanisms

**Independent Test**: Follow documented test procedures in quickstart.md and validate real-world usage patterns

### Manual Testing for User Story 4

- [ ] T073 [P] [US4] Test Git repository change detection and automatic redeployment using examples/readme-config.yaml
- [ ] T074 [P] [US4] Test multi-engine configuration (raw + kube + systemd + filetransfer) using examples/full-suite.yaml
- [ ] T075 [P] [US4] Test configuration file reloading without container restart
- [ ] T076 [P] [US4] Test disconnected operation mode with local archive using examples/full-suite-disconnected.yaml
- [ ] T077 [P] [US4] Test PAT token authentication using examples/pat-testing-config.yaml
- [ ] T078 [P] [US4] Test SSH key authentication for private repositories
- [ ] T079 [P] [US4] Test Podman secret authentication using examples/podman-secret-raw.yaml
- [ ] T080 [P] [US4] Test raw container deployment with capabilities using examples/raw-config.yaml
- [ ] T081 [P] [US4] Test Kubernetes pod deployment using examples/kube-play-config.yaml
- [ ] T082 [P] [US4] Test systemd unit deployment and enabling using examples/systemd-config.yaml
- [ ] T083 [P] [US4] Test file transfer operations using examples/filetransfer-config.yaml
- [ ] T084 [P] [US4] Test ansible playbook execution using examples/ansible.yaml
- [ ] T085 [US4] Document manual test results in specs/001-podman-v4-upgrade/functional-test-results.md
- [ ] T086 [US4] Verify zero regressions in existing functionality across all test scenarios

**Checkpoint**: User Story 4 complete - Manual functional testing validates all deployment mechanisms work correctly

---

## Phase 7: User Story 5 - Pull Request and Review Process (Priority: P3)

**Goal**: Create well-documented PRs for review and merge

**Independent Test**: Create PR and verify it contains required elements (description, test results, breaking change notes)

### PR Creation for User Story 5

- [ ] T087 [US5] Create PR description documenting all dependency version changes (v4.2.0 → v5.x)
- [ ] T088 [US5] Document Podman v5 breaking changes addressed in PR description
- [ ] T089 [US5] Include before/after test coverage metrics in PR description
- [ ] T090 [US5] Document Go version upgrade (1.17 → 1.21+) in PR description
- [ ] T091 [US5] Add code review checklist to PR description
- [ ] T092 [US5] Verify all GitHub Actions CI checks pass before requesting review
- [ ] T093 [US5] Request code review from at least 1 maintainer
- [ ] T094 [US5] Address reviewer feedback and re-test
- [ ] T095 [US5] Verify all integration tests pass after addressing feedback
- [ ] T096 [US5] Merge PR to main branch after approval

**Checkpoint**: User Story 5 complete - PR merged with comprehensive documentation and reviewer approval

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T097 [P] Update README.md with Go 1.21+ requirement
- [ ] T098 [P] Update README.md with Podman v5 compatibility notice
- [ ] T099 [P] Update documentation in docs/ for any API changes
- [ ] T100 [P] Add migration guide for users upgrading from v4 builds
- [ ] T101 [P] Update quickstart.md with Podman v5 specific instructions
- [ ] T102 Code cleanup: Remove any commented-out v4 code
- [ ] T103 Code cleanup: Ensure consistent error handling across all updated files
- [ ] T104 Verify backward compatibility with existing configuration files
- [ ] T105 Run full validation: `make build && go test ./... && manual functional tests`
- [ ] T106 Tag release with updated Podman v5 support

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup research completion (T001-T008) - BLOCKS all user stories
- **User Story 1 (Phase 3)**: Depends on Foundational phase completion (T009-T016)
- **User Story 2 (Phase 4)**: Depends on US1 completion (need working v5 code to test)
- **User Story 3 (Phase 5)**: Depends on US1 completion (need working v5 code for CI)
- **User Story 4 (Phase 6)**: Depends on US1, US2, US3 completion (need passing tests before manual validation)
- **User Story 5 (Phase 7)**: Depends on US1, US2, US3, US4 completion (need all work done for PR)
- **Polish (Phase 8)**: Depends on all user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - CRITICAL PATH
- **User Story 2 (P2)**: Depends on US1 (need working v5 code to write tests for)
- **User Story 3 (P1)**: Depends on US1 (need working v5 code for CI tests)
- **User Story 4 (P2)**: Depends on US1, US2, US3 (need passing automated tests before manual validation)
- **User Story 5 (P3)**: Depends on all other user stories (PR creation is final step)

### Within Each User Story

**User Story 1**:
- Research completion first (T001-T008)
- Dependency updates second (T009-T016)
- Core file fixes can happen in parallel (T017-T020 are [P])
- Import updates can happen in parallel after core fixes (T023-T031)
- Build and verification last (T032-T035)

**User Story 2**:
- All test creation tasks (T036-T053) can run in parallel [P]
- Test execution and coverage verification must be sequential (T054-T056)

**User Story 3**:
- Workflow updates first (T057-T062)
- Test job validation can run in parallel by pushing to branch (T063-T069)
- Final verification sequential (T070-T072)

**User Story 4**:
- All manual test scenarios (T073-T084) can run in parallel [P]
- Documentation and verification last (T085-T086)

**User Story 5**:
- PR creation tasks sequential (T087-T096)

### Parallel Opportunities

- **Phase 1**: T002, T003, T004, T005, T006 can all run in parallel [P]
- **Phase 2**: T011, T012, T013 can run in parallel [P]
- **Phase 3**: T017, T018, T019, T020 can run in parallel [P] (different files)
- **Phase 4**: T036-T053 can all run in parallel [P] (different test files)
- **Phase 6**: T073-T084 can all run in parallel [P] (independent test scenarios)
- **Phase 8**: T097-T101 can all run in parallel [P] (different documentation files)

---

## Parallel Example: User Story 1 Core Fixes

```bash
# Launch all core file fixes together (different files, no dependencies):
Task T017: "Fix Podman v5 API changes in pkg/engine/container.go"
Task T018: "Fix Podman v5 API changes in pkg/engine/raw.go"
Task T019: "Fix Podman v5 API changes in pkg/engine/image.go"
Task T020: "Fix Podman v5 API changes in pkg/engine/types.go"
```

## Parallel Example: User Story 2 Unit Tests

```bash
# Launch all unit test creation together (different test files):
Task T036: "Create unit tests for container creation in tests/unit/container_test.go"
Task T040: "Create unit tests for directory file transfer in tests/unit/filetransfer_test.go"
Task T042: "Create unit tests for systemd unit deployment in tests/unit/systemd_test.go"
Task T046: "Create unit tests for Kubernetes pod creation in tests/unit/kube_test.go"
Task T048: "Create unit tests for image operations in tests/unit/image_test.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup & Research (T001-T008)
2. Complete Phase 2: Foundational (T009-T016) - CRITICAL
3. Complete Phase 3: User Story 1 (T017-T035)
4. **STOP and VALIDATE**: Build project, verify v5 dependencies, test basic functionality
5. Can deploy/demo working Podman v5 build at this point

### Incremental Delivery

1. Setup + Research + Foundational → Podman v5 dependencies loaded, build errors documented
2. Add User Story 1 → Working Podman v5 build (MVP!)
3. Add User Story 2 → Comprehensive unit test coverage
4. Add User Story 3 → Full CI/CD pipeline with Podman v5
5. Add User Story 4 → Manual validation complete
6. Add User Story 5 → PR merged, feature complete

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Research together (Phase 1)
2. Team completes Foundational together (Phase 2) - MUST finish before splitting
3. Once Foundational done:
   - Developer A: User Story 1 (T017-T035) - CRITICAL PATH
   - Wait for US1 completion, then:
     - Developer B: User Story 2 (T036-T056) - Unit tests
     - Developer C: User Story 3 (T057-T072) - GitHub Actions
4. Once US1, US2, US3 complete:
   - Developer D: User Story 4 (T073-T086) - Manual testing
5. Once all stories complete:
   - Developer A: User Story 5 (T087-T096) - PR creation

---

## Task Execution Checklist

Before starting:
- [ ] Read specs/001-podman-v4-upgrade/spec.md for requirements
- [ ] Read specs/001-podman-v4-upgrade/plan.md for technical context
- [ ] Read specs/001-podman-v4-upgrade/quickstart.md for dev environment setup

During execution:
- [ ] Mark each task complete with ✓ when done
- [ ] Commit after each logical task group
- [ ] Run tests after each user story phase
- [ ] Document any blocking issues immediately

After User Story 1 (MVP):
- [ ] Build succeeds: `make build`
- [ ] Go mod shows v5: `go mod graph | grep podman`
- [ ] Basic functionality works locally

After User Story 2:
- [ ] All unit tests pass: `go test ./tests/unit/... -v`
- [ ] Coverage ≥60%: `go test ./... -cover`
- [ ] Tests run <30s

After User Story 3:
- [ ] All GitHub Actions jobs pass (100% pass rate)
- [ ] Tests complete <45 minutes
- [ ] Podman v5 binary cached successfully

After User Story 4:
- [ ] All manual test scenarios validated
- [ ] Zero regressions confirmed
- [ ] Test results documented

After User Story 5:
- [ ] PR created with complete documentation
- [ ] Code review completed
- [ ] PR merged to main

---

## Notes

- [P] tasks = different files, no dependencies - can run in parallel
- [Story] label maps task to specific user story for traceability
- User Story 1 is CRITICAL PATH - must complete before US2 and US3
- User Story 2 and 3 are both P1 priority but depend on US1 completion
- Verify tests fail/pass as expected before moving to next task
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Document any Podman v5 specific gotchas in research.md
- Keep backward compatibility with existing configuration files throughout
