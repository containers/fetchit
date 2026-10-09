Running
============

FetchIt connects to the Podman API socket on the host. Use Podman 5.7 or newer
within major version 5 for the documented host-managed features. Rootful and
rootless Podman stores are separate: build/pull helper images and create networks
in the same store used by FetchIt. See :doc:`samples` for amd64/arm64 applications
and :doc:`release_notes` for features requiring a newer engine/helper image.

Stopping the engine
-------------------

On current main, ``podman stop fetchit`` sends SIGTERM directly to the engine.
SIGINT is also supported. FetchIt stops scheduling work, cancels running
operations, and waits up to five seconds for jobs to finish before exiting.
Operations that do not honor cancellation may still be in flight when that
grace period expires; check the logs and workload state before restarting.
Stopping FetchIt leaves its deployed workloads running and preserves engine
state for the next start. Published releases require this shutdown fix to be
included in their engine image.

Rootless socket
---------------

Run as the host user that will own the containers:

.. code-block:: bash

   systemctl --user enable --now podman.socket
   export XDG_RUNTIME_DIR="/run/user/$(id -u)"

The socket is at ``$XDG_RUNTIME_DIR/podman/podman.sock``. For workloads that must
survive logout, enable lingering for that user:

.. code-block:: bash

   sudo loginctl enable-linger "$(id -un)"

Rootful socket
--------------

.. code-block:: bash

   sudo systemctl enable --now podman.socket

The rootful socket is ``/run/podman/podman.sock``. Access to either socket allows
administration of that Podman instance; only expose it to trusted engine images.

Launch manually
---------------

Create ``$HOME/.fetchit/config.yaml`` using :doc:`quick_start` or :doc:`methods`,
then launch a rootless engine:

.. code-block:: bash

   mkdir -p "$HOME/.fetchit"
   podman run -d --name fetchit \
     -v fetchit-volume:/opt \
     -v "$HOME/.fetchit:/opt/mount" \
     -v "/run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock" \
     --security-opt label=disable \
     quay.io/fetchit/fetchit:latest

FetchIt reads ``/opt/mount/config.yaml``. Keep the named volume to preserve Git
baselines and removal receipts across container recreation. Mounting a single
config file is possible for static configuration, but remote atomic reloads need
the writable directory mount above. For rootful operation, use ``sudo podman``
and substitute the rootful socket path.

Rootless tracked Systemd methods additionally need the actual host user's
``HOME`` and ``XDG_RUNTIME_DIR`` passed into the engine:

.. code-block:: bash

   -e HOME="$HOME" -e XDG_RUNTIME_DIR="/run/user/$(id -u)"

These are additional ``podman run`` options. Quadlet instead takes explicit
``hostHome``, ``hostConfigHome``, and ``hostRuntimeDir`` method fields; see
:doc:`quadlet`. Both require a running host systemd manager. See :doc:`lifecycle`
before enabling cleanup for existing files or services.

.. code-block:: bash

   podman logs -f fetchit

See :doc:`status` for optional HTTP liveness and schedule monitoring.

Launch with systemd
-------------------

From a repository checkout, install the service for the intended scope. Review
its image, configuration mount, and socket before starting it.

For root, the checked-in service uses root's ``~/.fetchit/config.yaml``:

.. code-block:: bash

   sudo mkdir -p /root/.fetchit
   sudo install -m 0600 /path/to/config.yaml /root/.fetchit/config.yaml
   sudo cp systemd/fetchit-root.service /etc/systemd/system/fetchit.service
   sudo systemctl daemon-reload
   sudo systemctl enable --now fetchit.service

For the host user, it uses ``$HOME/.fetchit/config.yaml``:

.. code-block:: bash

   mkdir -p "$HOME/.fetchit" "$HOME/.config/systemd/user"
   install -m 0600 /path/to/config.yaml "$HOME/.fetchit/config.yaml"
   cp systemd/fetchit-user.service "$HOME/.config/systemd/user/fetchit.service"
   systemctl --user daemon-reload
   systemctl --user enable --now fetchit.service

The user service already passes host ``HOME`` and ``XDG_RUNTIME_DIR``. These
services launch the engine; workload service configuration remains in its Git
methods. Restarting the engine does not automatically stop retained workloads.

Safe remote configuration updates
---------------------------------

``FETCHIT_CONFIG_URL`` and ``configReload.configURL`` accept a remote YAML config.
FetchIt requires HTTP 200, a single nonempty YAML mapping, and fields that decode
into the FetchIt configuration schema. Unknown fields, malformed YAML, empty
responses, and HTTP error pages are rejected. Requests time out after 30 seconds
and downloaded configs are limited to 1 MiB. Explicit empty lists such as
``targetConfigs: []`` are valid; an empty document is not.

A failed download or validation leaves ``config.yaml`` and its existing backup
unchanged. Config and backup are staged before replacement. Failed config replacement leaves
the old backup untouched; failed backup publication rolls the config back. A
rollback failure is reported explicitly and the installed config is loaded. The
two-file update is recoverable during ordinary errors, not a crash-atomic
transaction. Successful changed downloads save the previous bytes to
``config-backup.yaml`` and replace ``config.yaml`` using a staged file and atomic
rename, with mode 0600. Identical downloads do not trigger a reload. Validation
checks syntax and field decoding; it does not test network connectivity, image
availability, or every method's runtime requirements.

Mount a writable configuration **directory** at ``/opt/mount`` when using remote
reloads. A read-only mount or individual ``config.yaml`` file bind mount cannot
support atomic replacement: the update fails and preserves the current file.
On first startup without a local config, an invalid remote response does not
create a config file; correct the source and restart FetchIt. On scheduled
reloads, correct the source and FetchIt retries at the next configured interval.
