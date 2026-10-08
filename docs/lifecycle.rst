Workload cleanup and rollback
=============================

These features are Unreleased. Use an image built from a source revision that
includes them; older images do not recognize the new options. Both features are
opt-in. Their defaults preserve existing reconciliation behavior.

Cleaning up a removed method
----------------------------

Kube, Raw, and Quadlet methods accept ``cleanupOnRemoval: true``:

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads.git
     branch: main
     kube:
     - name: application
       targetPath: pods
       schedule: "*/5 * * * *"
       cleanupOnRemoval: true
     raw:
     - name: worker
       targetPath: containers
       schedule: "*/5 * * * *"
       cleanupOnRemoval: true
     quadlet:
     - name: services
       targetPath: quadlets
       root: true
       start: true
       schedule: "*/5 * * * *"
       cleanupOnRemoval: true

After the configuration has been loaded with this flag enabled, removing the
method (or its entire target) from configuration stops and removes its owned
workloads. FetchIt performs cleanup during configuration reload and on startup.
The reload waits for running deployment jobs to finish before retiring their
configuration; queued jobs from the old configuration do not deploy afterward.

For Kube, cleanup selects Pods with both FetchIt's managed-by label and the exact
method owner label. For Raw, it selects containers with those labels and the Raw
method-kind label (``fetchit.containers.io/method=raw``). The labels are checked again on the selected immutable IDs
before removal. Other methods' workloads and unlabeled/manual workloads are
retained. Labels are editable metadata; this is scoped lifecycle management,
not an authorization boundary against other users of the Podman socket.

Kube cleanup removes the Pods and their containers. It retains named volumes,
Secrets, ConfigMaps, images, and networks. Raw cleanup retains named volumes,
images, and networks. Quadlet cleanup stops services identified by its host
journal, removes its dedicated source bundle's contents, and reloads systemd.
It checks each stopped service's SourcePath against that bundle. Persistent
volumes and other resources are subject to the authored Quadlet units' own stop
behavior; cleanup does not separately prune them. Quadlet supporting files in
the method's bundle are removed together with its source units.

Persistence and recovery
~~~~~~~~~~~~~~~~~~~~~~~~

FetchIt records cleanup intent before scheduling an opted-in method. Receipts
live at ``/opt/.fetchit/method-removals.json`` in the persistent FetchIt volume.
They store method kinds, names, owner IDs, and the paths/image needed for Quadlet
host cleanup. They contain no Git URLs, Git credentials, manifests, decrypted
SOPS values, or age identities. The file is written atomically with mode 0600;
state I/O or configuration validation errors prevent starting untracked jobs.

Keep the FetchIt volume across upgrades and process/container recreation. This
lets FetchIt clean a removed method even if it was removed while FetchIt was
stopped. Missing state cannot be reconstructed by scanning every Pod: FetchIt
will not remove unknown workloads. Do not delete the state file as a cleanup
step; doing so relinquishes that cleanup responsibility.

Cleanup failures are logged and the receipt is retained. FetchIt retries once
per minute and on subsequent startup/reload. Cleanup is idempotent; already
removed resources do not need to be recreated for a retry. Reintroducing the
same method identity before a retry makes it active again and prevents removal.
An interrupted deployment may leave partially created resources; they are still
eligible for removal if they have the recorded method's ownership labels or
Quadlet journal entries.

A method with ``cleanupOnRemoval: false`` (the default) retains its workloads
when it leaves configuration. To relinquish cleanup for an opted-in method,
first deploy configuration with the flag set to false while that method is still
present; then remove the method in a later configuration update.

Upgrade and identity changes
~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Legacy Raw containers without the new owner and method-kind labels, and Kube
Pods created before workload labeling was added, are retained. Recreate those
workloads through FetchIt before removing their opted-in methods. Labels are added during successful recreation, including replay of the saved
revision during Kube/Raw startup reconciliation. Without opting in, the existing
procedure remains available: delete definitions from Git first and wait for a
successful reconciliation before removing their configuration methods.

Changing the repository URL, branch, method name, or target path changes Kube/Raw
ownership. With cleanup enabled, this retires the old identity and starts the
new one. A Quadlet ownership identity additionally distinguishes its host paths;
activation changes such as start/restart retain its bundle ownership. Use unique
resource names when methods share a Podman instance. Configurations with duplicate
lifecycle identities are rejected.

Ansible, FileTransfer, legacy Systemd, Prune, Image, and ConfigReload do not support
``cleanupOnRemoval``. Their operations cannot be generically reversed by stopping
an owned Pod/container or removing a dedicated bundle. Enabling the flag for
these methods is rejected. A method's removal does not remove Git cache data.

Rollback after a failed apply
-----------------------------

Rollback is configured per Git target and applies to Raw, Kube (including SOPS),
and Quadlet methods:

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads.git
     branch: main
     rollback: true
     trackBadCommits: false
     kube:
     - name: application
       targetPath: pods
       schedule: "*/5 * * * *"
       cleanupOnRemoval: true

On an apply error, FetchIt attempts to apply the changes in reverse, restoring
the previously applied Git revision. On a first deployment, the previous state
is empty: rollback removes the newly attempted workloads. File contents come
from immutable Git blobs, including historical deleted files. Ordinary changes
run in deterministic filename order. The applied Git tag advances only after a
successful forward apply; a failed apply or rollback leaves that tag unchanged.

Rollback is best effort. It is not transactional, does not check application
health, and cannot undo external effects such as writes to persistent volumes.
Both the apply and rollback failures remain available as typed error causes;
logs report the recovery outcome without embedding potentially sensitive API
response content. If rollback fails, inspect the workloads, repair the underlying
problem, and let reconciliation retry. Kube SOPS preparation failures guarantee
no mutation, so they do not trigger unnecessary teardown/rollback.

``trackBadCommits: true`` requires ``rollback: true``. After successful rollback,
it suppresses another attempt at the same failed head for that method and its
applied baseline. Another method on the same target can still attempt that
commit. Failure to roll back never suppresses retries. Tracking is bounded,
process-local state, reset by process/config reload; it is not persisted as a
permanent Git blacklist. With tracking disabled, FetchIt retries normally, which
is useful for transient registry/network failures. Disable tracking or reload
FetchIt to retry a repaired environment without making a new Git commit.

Rollback defaults to false. It is rejected for targets containing methods other
than Raw, Kube, or Quadlet because arbitrary Ansible/file-copy/systemd side effects
are not safely reversible through this mechanism.

Regression coverage
-------------------

GitHub Actions runs these lifecycle tests for pull requests and main pushes:

* The Podman matrix exercises Ubuntu rootful, Ubuntu rootless, and Fedora userspace
  with Podman 5. It checks real configuration download/reload cleanup, persisted
  receipts after process reconstruction, unrelated-container preservation, Raw
  Git deletion (including its last file), and Kube partial/initial apply rollback.
* The Quadlet rootful/rootless matrix checks recovery from a failed systemd start
  and cleanup of a configured bundle using its host journal.
* Unit tests check failed-cleanup retries, opt-out/reintroduction, ownership
  isolation, invalid receipt state, retirement synchronization, and rollback
  failure/bad-commit tracking.

Runtime fixtures use disposable local Git repositories and require no repository
write permissions or shared test branches. See ``.github/workflows/sops.yml`` and
``.github/workflows/quadlet.yml`` for environment setup. Fedora coverage uses a
privileged nested container on a disposable runner; it verifies Fedora userspace,
not native host SELinux policy enforcement. Rootless Ubuntu uses host networking
for fixtures to avoid the runner's network-helper termination restrictions.
