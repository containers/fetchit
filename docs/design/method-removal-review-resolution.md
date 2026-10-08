# Method cleanup and rollback adaptation

The PR implements #252 (opted-in method removal), fixes the still-present #213
Raw delete-file bug, and rebases/adapts the rollback behavior from #209.

Historical #209 review decisions:
- Use the existing Zap logger; no klog dependency or module downgrade is added.
- Configuration fields are exported and decoded by the normal config parser.
- Failed-commit tracking is synchronized and scoped per method and applied
  baseline, bounded, and process-local. It does not suppress another method.
- Keep rollback opt-in: automatic reverse application changes lifecycle behavior
  and cannot undo arbitrary external effects. The old reviewer suggestion to
  enable it by default is deliberately declined to preserve compatibility.
- First-deployment rollback can target an empty Git tree. Deleted directories and
  historical blobs are supported. Raw deletion does not read a file named delete.
- Preserve the original apply error and any rollback error. Do not advance the
  applied tag on either failure. A preparation-only SOPS failure does not tear
  down unchanged workloads or blacklist a transient key failure.
- Read forward/reverse Raw and Kube inputs from Git blobs instead of checking out
  another commit into a shared mutable worktree.
- CI uses disposable local repositories and actual Podman/systemd operations on
  pull requests, without force-pushing fixture commits or rebuilding Podman.
- Original rollback author attribution and sign-off are retained by rebasing the
  commit locally and incorporating the adapted commit into this feature branch.

Cleanup boundaries:
- Default retention is preserved; enable cleanupOnRemoval while the method still
  exists. Register receipts durably before scheduling work, including intent for
  resources left by a partially failed deployment.
- Serialize deployment-job retirement against reload; a ConfigReload job does
  not hold the deployment gate while waiting for other jobs to finish.
- Select labeled Kube Pods or Raw containers by owner and immutable ID. Retain
  non-workload resources and unlabeled legacy/manual resources.
- Quadlet cleanup uses its existing namespaced host journal and SourcePath checks,
  even when the Git repository/old commit is unavailable. Cleanup mode bypasses
  revision CAS only for that retirement operation, under the existing host lock.
- Receipt I/O/config validation errors prevent untracked starts. Runtime cleanup
  failures retain receipts and retry; reintroduction cancels pending removal.
- Legacy Systemd, Ansible, and file-copy methods are not generically reversed.

See docs/lifecycle.rst for the operator-facing policy and regression coverage.
