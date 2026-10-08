Release notes
=============

Unreleased
----------

HTTP image and archive download errors
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Image downloads and disconnected ZIP archive downloads now report an error when
the server returns anything other than HTTP 200, including 204, 401, 404, and
500. Error responses are not written or imported. Response bodies are closed
even when a download is skipped because its local file already exists. Archive
body read failures and invalid ZIP files also return errors.

Optional Git repository mirrors
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Git targets may set an ordered ``fallbackURLs`` list while retaining ``url`` as
the primary identity. Clone and fetch retry trusted mirrors after primary failures;
stale/divergent fallback histories are rejected for existing checkouts. Existing
single-source behavior is unchanged. See :doc:`mirrors` for authentication, local
mounts, history rules, initial clone behavior, and recovery.

Quadlet bundles
~~~~~~~~~~~~~~~

FetchIt now supports the ``quadlet`` method for host-managed Podman 5 services.
It deploys complete Git bundles, validates with the installed host generator,
preserves drop-ins and relative supporting files, and handles updates, service
renames, directory removal, and retries after partial application. Rootful and
rootless operation require host systemd and cgroup v2. See :doc:`quadlet` for the
complete configuration, setup walkthroughs, inspection commands, and limitations.

New method fields include ``root``, ``start``, ``restart``, ``hostHome``,
``hostConfigHome``, ``hostRuntimeDir``, and ``helperImage``. Rootless paths refer to
the host user's identity, not FetchIt's container environment. Configuration
settings participate in applied-state identity, so activation changes are applied
on reload. Changing deployment scope or paths requires cleaning up the old bundle
with its previous configuration first.

The helper uses a limited capability set and host PID namespace. It exposes the
host binaries read-only and the configuration parent writable; use trusted images
and Git repositories. Bundles are limited to 32 MiB and 4,096 files. The feature
has container/network/volume lifecycle coverage in rootful and rootless GitHub
Actions jobs on Ubuntu 26.04, with packaged Podman rather than per-run source builds.

Dependencies
~~~~~~~~~~~~

The OpenTelemetry OTLP HTTP trace exporter is updated from 1.44.0 to 1.45.0,
including the matching OpenTelemetry core, metric, trace, and SDK modules selected
by Go's dependency graph. This incorporates dependency PR #395. Podman remains on
major version 5, currently the v5.8.8 Go libraries, with the existing OCI runtime
specification compatibility replacement retained.

CI reliability
~~~~~~~~~~~~~~

The existing secret-config integration now polls running workload names while a
configuration reload recreates containers, rather than failing on a transient
single snapshot during reconciliation.

Optional health and status HTTP endpoint
----------------------------------------

Set ``FETCHIT_STATUS_ADDR`` to enable ``/healthz`` and ``/status``. The status
response includes current method schedules and invocation statistics, including
Quadlet methods. Snapshots are safe during concurrent reconciliations and config
reloads. See :doc:`status` for deployment and monitoring details.


SSH integration, remote config safety, and optional networks
------------------------------------------------------------

SSH CI now uses a temporary local Git server to verify host-key rejection,
clone, fetch, and file deployment without GitHub secrets. Clone failures now
propagate to the caller, allowing scheduled retries to report the actual error.

Remote configuration downloads reject HTTP failures and invalid YAML before
changing files, fixing issue #345. Updates use atomic replacement and require a
writable directory mount; see :doc:`running` for limits and recovery.

Raw and Kube accept optional existing Podman networks (issue #285). Omitted or
empty network lists preserve the existing behavior; Raw manifests may override
the method list. See :doc:`methods` for setup and redeployment behavior.
