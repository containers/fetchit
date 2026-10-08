# Release cleanup review

Reviewed the engine, command entry point, unit tests, and existing lifecycle workflows against main at 81bdb42. This is a source review and targeted regression pass; it does not establish that every supported deployment mode is release-ready.

## Changes included

- Raw, Kube, and Ansible repeated the same target locking, clone, saved-revision replay, latest-revision apply, and startup retry logic. They now use one common implementation. FileTransfer and legacy Systemd retain their differing startup behavior; Quadlet retains its bundle/receipt reconciliation.
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

1. **High: disconnected-device copy and config replacement can fail silently.** `localDevicePull` treats a nil inspect result as an existing container, including the normal missing-container error path. It discards wait/removal and cache-write failures; `currentToLatest` and `getDeviceDisconnected` discard its returned error. `loadDevicePodman` waits again on a container that the copy helper already removed. `checkForDisconUpdates` ignores several filesystem/helper errors and replaces files without the validation and atomic publication used by HTTP config updates. Test device-present/absent transitions, copy failure, invalid configuration, and interrupted replacement with a disposable block device before relying on this mode for a release.
2. **High: legacy helper commands concatenate paths into shell commands.** The metacharacter denylist does not handle quoting, whitespace, or option-like paths; Ansible constructs its command without that check. Replace command strings with argument vectors where possible, use positional shell arguments for compound commands, and test filenames containing spaces, quotes, and leading dashes. The current validation must not be treated as a complete shell-safety boundary.
3. **Medium: FileTransfer teardown chooses the new filename.** `MethodEngine` takes `change.To.Name` for removal. A deletion has no new filename and a rename leaves the old destination behind. Legacy Systemd also needs careful stop-before-delete ordering. Add real host-directory and service lifecycle tests for both methods, including rename, deletion, and retry. Current Raw/Kube/Quadlet removal coverage does not cover them.
4. **Medium: download and single-source Git operations have weak cancellation.** Image/archive downloads use `http.Get` without an application timeout; single-source Git fetch uses an unrelated background context. Slow or stalled operations can delay configuration retirement. Introduce operation contexts and bounded timeouts while preserving large-transfer support, with stalled-server tests.

## Deliberately retained

- `utils.FetchImage` has no repository caller but is exported. Removing it would change the public Go API; it overlaps the force-aware engine image helper and can be deprecated separately.
- `rawPodman` and `kubePodman` are used by preflight/deletion tests. They remain small file-based adapters to the same production apply routines.
- No-op `Apply`/`MethodEngine` methods on non-Git methods satisfy `Method`; they are not unreachable functions.
- Podman 5, dependency versions, configuration defaults, cleanup ownership rules, SOPS handling, and Quadlet host journaling are unchanged.

## Regression coverage

`TestCommonGitProcessRetriesAndAdvancesOnlyOnSuccess` exercises clone/fetch against local Git repositories, failed apply retry, saved-revision replay, unchanged-head skipping, and successful tag advancement. `TestRollbackUnrecordedEmptyStateIsApplied` covers the absent-entry regression. `TestWrapErrPreservesCause` covers error identity and type recovery. `TestHelperContainerCompletion` uses a local mock Podman HTTP service to exercise successful/failed commands and verified/unverified malformed removals without a host runtime.

The existing Ubuntu/Fedora unit workflows run these tests on each PR. The race job includes common reconciliation and lifecycle gates. Existing rootful/rootless Podman and Quadlet workflows provide runtime coverage for deployed workloads; a mock API test does not replace those runs.
