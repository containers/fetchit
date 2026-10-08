# Quadlet PR review decisions

This records the review of PR #397. User instructions and the supported runtime
contract take precedence over automated suggestions.

- **Stale concurrent plans:** the helper compares the expected Git revision with an
  atomic host receipt while holding its lock. Stale plans fail before changing files.
- **Bundle and configuration identities:** tags include branch, bundle path, host
  scope/paths, activation settings, and helper image. Host directory identity includes
  branch and bundle path while remaining stable across activation changes. Moving a
  deployment requires cleanup with its old configuration first.
- **Restart/recovery:** initialization checks the host receipt, including pending
  application state, even when the applied tag matches HEAD. Rollback commits repair
  partially installed bundles. An unchanged successful receipt performs no lifecycle
  operations. This is separate from continuous drift repair.
- **Memory:** bundles are bounded to 32 MiB and 4,096 files, checked before loading
  blob contents. This initial implementation deliberately bounds the in-memory tar
  approach; streaming larger build contexts remains future work.
- **Host access:** the helper uses a small capability set instead of privileged mode,
  a read-only host root, and one writable configuration parent. These mounts allow
  the actual host generator and libraries to validate a bundle. Rootful deployment
  is explicitly selected with `root: true`. Quadlet workloads and helper images
  remain trusted host administration inputs; the docs recommend digest-pinned images
  without breaking local builds or existing default-image conventions.
- **Utilities and cleanup:** helper recipes explicitly install/find required tools;
  helper-removal API errors and per-container errors propagate rather than being
  swallowed. Copies preserve executable modes without propagating image SELinux
  labels/xattrs onto host configuration files.
- **Documentation:** complete setup, rootless identity, generated names, inspection,
  updates, removal, recovery, configuration changes, and limitations are documented.
  The helper's ten-argument contract and generator delimiter format are described
  beside their implementation. Historical design notes live under `docs/design`.

Several automated findings do not describe the actual behavior:

- Deleting a directory in Git leaves a **nonzero commit hash**. The missing desired
  subtree becomes an empty bundle and is tested; a zero desired hash is invalid input.
- Ubuntu 26.04 is already running this repository's jobs. Both real lifecycle modes
  passed on commit 2185876 before the subsequent review changes.
- Podman 5 prints delimiters such as `---web.service---`, without spaces. This was
  checked against [upstream source](https://github.com/containers/podman/blob/v5.8.8/cmd/quadlet/main.go)
  and the live generator in both CI modes.
- A POSIX pipeline's status is its **last command's** status. A failing final `while`
  loop fails the pipeline under `sh -e`; the claimed left-hand `printf` success does
  not swallow it. Validation and start failure/retry tests exercise this behavior.
- On the tested systemd version, querying a missing unit's SourcePath/FragmentPath
  returns empty values. New-service creation passed in both host modes. Other bus
  errors should remain fatal, rather than being suppressed as if a unit were absent.

Workflow duplication and further extraction of small orchestration functions were
considered readability suggestions, not merge blockers. Explicit mode-specific
commands keep privilege boundaries visible in CI.


## Status endpoint carried forward from PR #382

The two Go changes from #382 add useful opt-in monitoring and are included here,
with the following resolutions to its Sourcery review:

- Copy method values under the registry mutex before sorting/encoding; run time
  pointers refer to immutable timestamps. Concurrent snapshot tests cover this
  with the race detector.
- Omit an uninitialized start time and report zero uptime with `initializing`.
  Registering even an empty configuration establishes the process start time.
- Log JSON and health response write errors. Do not attempt a second HTTP error
  response after a write has begun; it cannot reliably change the status code.
- Configure explicit header/read/write/idle timeouts and document interface
  binding, authentication limits, and loopback container publishing.
- Keep the scheduler callback's two `context.Context` parameters. Sourcery's
  proposed `*grpc.ClientConn` does not match this repository: `Fetchit.conn` and
  `Method.Process` both use Podman binding contexts, not gRPC client connections.
- Start one listener per process and replace the registry on config reload so
  removed methods disappear while retained identities preserve counters.

Only the requested Go functionality and its tests/docs are carried forward;
#382's separate dependency automation workflows are outside this change.
