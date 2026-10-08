# Quadlet support: implementation and landing plan

This document records the original design review. The implementation is now in `pkg/engine/quadlet.go` and `quadlet-host.sh`; see `docs/quadlet.rst` for the shipped contract. It reviews [PR #367](https://github.com/containers/fetchit/pull/367) against current Fetchit main and Podman v5.8.8. Keep Fetchit on Podman 5.

## Architecture

Add a `quadlet` method alongside `systemd`, using `CommonMethod`, the existing Git reconciliation/state machinery, and helper containers for host operations. Deploy source files into the host's Quadlet search path and ask the host systemd manager to reload. The host's installed Podman generator produces services; Fetchit then starts or restarts the requested services.

Do not use the local Podman engine to install files inside the Fetchit container. Podman 5's remote engine explicitly returns “not implemented for the remote Podman client” for Quadlet install/list/print/remove. There is no corresponding Quadlet binding in the currently vendored socket-client packages. [Upstream remote implementation](https://github.com/containers/podman/blob/v5.8.8/pkg/domain/infra/tunnel/quadlet.go).

Use the APIs already supplied by our pinned dependency:

- [`parser.ParseUnitFile`, or `NewUnitFile` plus `Parse`](https://pkg.go.dev/github.com/containers/podman/v5@v5.8.8/pkg/systemd/parser) to parse source files. For Git historical contents, set `Filename` explicitly before resolving names.
- [`quadlet.IsExtSupported` and `GetUnitServiceName`](https://pkg.go.dev/github.com/containers/podman/v5@v5.8.8/pkg/systemd/quadlet) to recognize types and honor `ServiceName=`. `GetUnitServiceName` returns the name without `.service`; append the suffix once. Parse old and new revisions separately so a changed `ServiceName=` retires the old unit.
- The conversion functions are available for testing or diagnostics, but do not deploy locally generated `.service` files. The host generator is authoritative for the host's Podman version, dependencies, search paths, and drop-ins.

Parsing alone does not validate every Quadlet option. Before changing live files, validate a staged bundle with the host generator's `--dryrun` using `QUADLET_UNIT_DIRS`; capture diagnostics and verify expected services were generated. Check the host supports Podman 5.7–5.x, has the generator and systemd manager available, and uses cgroup v2. Library-version checks alone are insufficient. [Podman 5.8 manual](https://docs.podman.io/en/v5.8.0/markdown/podman-systemd.unit.5.html).

## Corrections required in PR #367

| Finding | Required behavior |
| --- | --- |
| The service loop skips changes with no `To.Name`, making its delete branch unreachable. | Stop retired units before removing their source files and reloading. |
| Start/restart failures are logged and swallowed. | Return failures from `Apply`; the existing state machinery must not record a failed commit as applied. |
| Rename has file handling but no matching service lifecycle. | Compare old and new service identities, retire the old unit, and start the replacement. |
| `.build`, `.image`, and `.artifact` use incorrect service names; overrides are ignored. | Use upstream naming helpers. Defaults include `-build`, `-image`, and `-artifact`. |
| Filtering only the eight Quadlet suffixes excludes YAML, build contexts, environment files, and `.conf` drop-ins. | Reconcile a dedicated bundle directory while distinguishing service sources from supporting files. |
| Existing FileTransfer uses basenames, flattening nested paths. | Preserve relative paths, including drop-in directories and relative references. |
| Rootless defaults use the Fetchit container's UID/environment. | Require or reliably discover the host user's home, UID, and runtime directory; test the actual host user manager. |
| Directory creation interpolates paths into a shell command. | Use argument vectors and narrow bind mounts; validate destination paths. |
| The new type shadows `CommonMethod.initialRun`. | Keep one inherited field and the normal scheduler initialization. |

Avoid changing existing Systemd behavior as a side effect. Introduce a separate, explicit helper action contract for Quadlet reload/start/restart/stop, with exit statuses propagated. The current Systemd stop action also removes unit files; Quadlet stopping must not do that. Build and test the helper image from the same change rather than relying on an existing `:latest` image to contain new actions.

## Reconciliation contract

1. Read the applied and desired Git trees, including historical source contents. Build an ordered plan for the entire selected bundle; detect duplicate generated service names and destination collisions.
2. Stage all desired files, preserving their relative layout. Validate the complete bundle using the host generator before stopping anything or changing live files. Reject symlinks or explicitly constrain their targets to the managed bundle.
3. Stop removed or renamed services while their old definitions still exist. A `ServiceName=` change is also a rename.
4. Install changed files and remove only files owned by this method. Reload systemd once after the complete batch. Preserve enough state to retry interrupted or partially successful operations.
5. Start new services and restart changed services according to configuration. Let systemd resolve dependencies. Supporting-file changes must trigger the affected service, or conservatively all managed services when restart is requested.
6. Return any failure. Advance Fetchit's applied Git hash only after all required operations succeed. Retrying the same commit must repair partial application without treating an already stopped unit as an unrecoverable failure.

An unchanged commit remains a no-op under Fetchit's existing Git semantics. Continuous drift repair would be a separate feature.

Use `start` and `restart` configuration names for this new method. Boot activation belongs in the Quadlet's `[Install]` section; generated services cannot be activated at boot through `systemctl enable`. If retaining #367's `enable` field for compatibility, document it strictly as immediate start, not boot enablement. [Upstream activation rules](https://docs.podman.io/en/v5.8.0/markdown/podman-systemd.unit.5.html#enabling-unit-files).

Rootful files belong under `/etc/containers/systemd`; rootless files belong under the host user's `$XDG_CONFIG_HOME/containers/systemd` or `~/.config/containers/systemd`. Use a method-owned subdirectory and avoid overwriting units belonging to another method. The manual documents recursive search and rootless paths. A user manager and lingering are deployment prerequisites when rootless workloads must survive logout.

## Reviewable implementation sequence

1. **Planning and unit tests:** config/scheduler registration; bundle layout; upstream parsing/name resolution; deterministic old/new lifecycle plans; injectable host operations. Cover all eight names, overrides, supporting files, rename, deletion, collisions, and retry behavior.
2. **Rootful execution:** staging, host validation, file installation, helper actions, reconciliation and failure propagation. Ship `.container`, `.network`, and `.volume` examples first. Only advertise additional types after exercising their resource and supporting-file behavior.
3. **Rootless execution:** explicit host identity and user-manager access, plus tests with a real non-root host user. Avoid silently guessing UID 1000 or using `os.Getuid()` inside Fetchit.
4. **Integration and user documentation:** concise method reference and runnable examples matching the actual `TargetConfig` shape. Reuse the useful scenarios from #367 without importing its large generated documentation set.

## Required verification before merging implementation

- Fake-host reconciliation tests assert operation order, a single reload per batch, no action for unchanged commits, and unchanged applied hash on validation/copy/reload/start/restart/stop failures.
- A failed partial application followed by a retry converges. Deletion and rename leave no running orphan service. Name overrides and nested supporting files work.
- Rootful and rootless integrations on Fedora 44 and the Ubuntu 26.04 GitHub runner use packaged Podman 5 and the installed generator, without compiling Podman per run.
- Container plus network/volume dependency integration; drop-in and environment-file updates; `.kube` YAML and `.build` context deployment before advertising those types.
- Host compatibility failures provide actionable diagnostics, including generator output. New helper actions must report oneshot success correctly rather than requiring every service to remain active.
- Existing unit, Systemd, disconnected Git, and integration suites continue passing. Linux CI must exercise real systemd behavior; macOS unit tests alone cannot establish host compatibility.

The new `quadlet.yml` workflow exercises both host modes on Ubuntu 26.04. Fedora 44 runs the unit suite; real Fedora host integration remains a follow-up verification task. The merge gate is successful Git-to-host lifecycle reconciliation, including cleanup and retries, on both tested host modes. Supporting all extensions by filename alone is not sufficient.
