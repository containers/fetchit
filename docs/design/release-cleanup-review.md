# Release cleanup review

Reviewed the engine, command entry point, unit tests, and existing lifecycle workflows against main at 81bdb42. This is a source review and targeted regression pass; it does not establish that every supported deployment mode is release-ready.

## Changes included

- Raw, Kube, and Ansible methods duplicated the same target locking, clone, saved-revision replay, latest-revision apply, and startup retry logic. They now use one common implementation. FileTransfer and legacy Systemd retain their differing startup behavior; Quadlet retains its bundle/receipt reconciliation.
- Raw, Ansible, FileTransfer, and Systemd repeated the same Git diff and change dispatch. A common helper now owns that sequence; Kube keeps its SOPS batch preparation.
- Removed the unused `Fetchit.volume`, `restartFetchit`, and write-only `allMethodTypes` bookkeeping, the configuration-only mutex that held locks until the entire registration loop returned, and Raw's unreachable second deletion branch. Runtime target and retirement locks remain.
- Removed process-wide `ROOT` writes from legacy Systemd helpers. Each helper already receives the correct value in its own container environment; concurrent rootful/rootless methods should not change the daemon's environment.
- Container inspections used only for existence/identity no longer request filesystem-size calculation. Raw conversion slices reserve their known output size while retaining non-nil empty slices.
- Signature verification now consumes and closes the full Git object reader instead of relying on a single read. Signature bytes no longer pass through an unnecessary reader and copy.
- Error wrapping now retains causes for `errors.Is`/`errors.As`, with readable errors when the cause is nil. Ordinary non-nil error messages are unchanged.
- Bad-commit tracking distinguishes a recorded failed empty revision from a missing map entry.
- Helper-container completion now reports nonzero exit status and still attempts cleanup. The duplicated malformed-removal workaround now succeeds only after inspection confirms HTTP 404; failed verification and removal-report errors remain failures. This changes what callers that check the helper result report; callers that discard it are identified below.

## Remaining findings, ordered by risk

These need targeted fixes and integration coverage, rather than being folded into shared-code cleanup without validation.

1. **Addressed during PR review: disconnected-device copy and config replacement could fail silently.** `localDevicePull` treats a nil inspect result as an existing container, including the normal missing-container error path. It discards wait/removal and cache-write failures; `currentToLatest` and `getDeviceDisconnected` discard its returned error. `loadDevicePodman` waits again on a container that the copy helper already removed. `checkForDisconUpdates` ignores several filesystem/helper errors and replaces files without the validation and atomic publication used by HTTP config updates. The PR now accepts only typed 404 for absent helpers, propagates copy/cleanup/cache errors, removes the redundant wait, and validates and atomically publishes device configurations. Mock API tests cover absent/existing/failed inspection, failed copy, and helper cleanup; configuration tests cover invalid/oversized input and backup publication. Physical block-device transition coverage remains a follow-up.
2. **High: legacy helper commands concatenate paths into shell commands.** FileTransfer now uses direct `rsync -avz -- source destination` and `rm -f -- path` argument vectors. The remaining device-command metacharacter denylist does not handle quoting, whitespace, or option-like paths; Ansible constructs its command without that check. Replace command strings with argument vectors where possible, use positional shell arguments for compound commands, and test filenames containing spaces, quotes, and leading dashes. The current validation must not be treated as a complete shell-safety boundary.
3. **Partially addressed: FileTransfer teardown chose the new filename.** `MethodEngine` takes `change.To.Name` for removal. A deletion has no new filename and a rename leaves the old destination behind. Legacy Systemd also needs careful stop-before-delete ordering. Add real host-directory and service lifecycle tests for both methods, including rename, deletion, and retry. FileTransfer now removes `change.From.Name`, skips removal on first creation, and makes missing-file removal idempotent. API tests assert operation order and propagation of removal failure; existing Actions cover actual initial copies. Legacy Systemd stop/delete ordering and real rename/delete coverage remain follow-ups.
4. **Medium: download and single-source Git operations have weak cancellation.** Image/archive downloads use `http.Get` without an application timeout; single-source Git fetch uses an unrelated background context. Slow or stalled operations can delay configuration retirement. Introduce operation contexts and bounded timeouts while preserving large-transfer support, with stalled-server tests.

## Deliberately retained

- `utils.FetchImage` has no repository caller but is exported. Removing it would change the public Go API; it overlaps the force-aware engine image helper and can be deprecated separately.
- `rawPodman` and `kubePodman` are used by preflight/deletion tests. They remain small file-based adapters to the same production apply routines.
- No-op `Apply`/`MethodEngine` methods on non-Git methods satisfy `Method`; they are not unreachable functions.
- Podman 5, dependency versions, configuration defaults, cleanup ownership rules, SOPS handling, and Quadlet host journaling are unchanged.

## Regression coverage

`TestCommonGitProcessRetriesAndAdvancesOnlyOnSuccess` exercises clone/fetch against local Git repositories, failed apply retry, saved-revision replay, unchanged-head skipping, and successful tag advancement. `TestRollbackUnrecordedEmptyStateIsApplied` covers the absent-entry regression. `TestWrapErrPreservesCause` covers error identity and type recovery. `TestHelperContainerCompletion` uses a local mock Podman HTTP service to exercise successful/failed commands and verified/unverified malformed removals without a host runtime.

The existing Ubuntu/Fedora unit workflows run these tests on each PR. The race job includes common reconciliation and lifecycle gates. Existing rootful/rootless Podman and Quadlet workflows provide runtime coverage for deployed workloads; a mock API test does not replace those runs.

## PR review corrections

Sourcery feedback is covered by explicit revision-pair assertions, a stateful helper-removal mock tied to the expected container ID, wait-failure cleanup, typed JSON syntax errors for malformed response compatibility, and a cancellable skew timer. The small shared Git coordinator keeps early returns and existing phase helpers (`getRepo`, `zeroToCurrent`, `currentToLatest`); additional forwarding helpers would not reduce its orchestration complexity.

For the remaining shell-command finding, compound commands should use quoted positional parameters, such as `sh -c 'mount -- "$1" /mnt && rsync -avz -- "$2" "$3"' sh device source destination`. For legacy Systemd deletion, select `change.From.Name`, stop that service, then unlink its file. For cancellation, use `http.NewRequestWithContext(ctx, ...)` and an explicit HTTP client timeout, and pass the caller's derived operation context to `repo.FetchContext(ctx, options)`.
