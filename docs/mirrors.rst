Repository mirrors and failover
===============================

Git targets can optionally configure ordered fallback repositories. The existing
``url`` string remains the primary source; omit ``fallbackURLs`` to keep the
existing single-source behavior:

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads.git
     fallbackURLs:
     - https://gitlab.com/example/workloads-mirror.git
     - file:///opt/mirrors/workloads.git
     branch: main
     raw:
     - name: web
       targetPath: raw
       schedule: '*/1 * * * *'

Configure a real synchronized mirror containing the same branch and commit
history. FetchIt tries the primary first on every clone/fetch, then fallbacks in
order after failure. Empty and duplicate fallback entries are ignored. A list with no distinct fallback
uses the existing single-source path and its existing timeout behavior. Each
attempt has a 30-second timeout for transport and configured signature verification
when failover is enabled. A missing
branch, transport/authentication failure, or failed configured signature check
allows the next source to be tried. Without this feature, existing single-source
transport behavior remains unchanged.

The primary URL continues to determine the local repository directory, applied
Git tags, and Quadlet bundle identity. A clone obtained from a mirror still stores
the primary as ``origin``. Adding, removing, or reordering fallback URLs does not
move this cache. Keep the primary identity unchanged when using mirrors. Changing
its repository basename follows existing cache behavior and is not a migration
mechanism.

For an existing checkout, a fallback must have the exact same branch commit or a
descendant of the local branch. Stale and divergent mirrors are rejected before
moving the branch or checkout. If every source fails, FetchIt keeps its current
checkout and applied workload state and retries on the next scheduled invocation.
Successful equal commits count as an available source, so later mirrors are not
queried for fresher content. Keep the mirror order consistent with your intended
priority. A recovered primary takes priority on the next run, including its
existing force-fetch behavior for intentional history rewrites and rollbacks.

The first clone has no local history to compare, so a fallback's branch is accepted
as the baseline. Enable the existing commit-signature verification when required;
it remains enforced for all sources. Signature verification service availability
is a separate dependency and is not eliminated by repository mirrors.

Authentication and local mirrors
--------------------------------

The existing ``gitAuth`` configuration applies to every listed source. All URLs
must be trusted to receive that configured credential. Do not mix unrelated hosts
that require different credentials; per-source credentials are not supported.
Authenticated plain HTTP is rejected before any source is contacted. HTTPS URLs
with embedded user information and unsupported transport schemes are rejected.
Unauthenticated HTTP remains available; HTTPS, verified SSH, and explicitly
configured absolute local ``file://`` paths are supported. Configured URLs are
the explicit trust list; there is no separate host allowlist. Avoid embedding
credentials in URLs. Source attempts are reported by their list
position in new failover errors to avoid logging credentials from transport errors.
Use Git-provider and SSH-server diagnostics for underlying transport details.

With SSH authentication, use SSH URLs for the remote sources and add each
provider's verified host key to ``/opt/mount/.ssh/known_hosts``. The configured
private key must be authorized on each server. Unknown or changed SSH host keys
are rejected; do not disable checking to make failover work.

For ``file://`` sources, mount the mirror into the FetchIt container at the exact
configured path and make it readable. An existing host clone is not automatically
visible inside the container. The local mirror must be updated independently;
FetchIt does not synchronize mirrors.

This option applies to Git-backed methods, including Raw, Kube, file transfer,
systemd, Ansible, and Quadlet. It does not add fallback HTTP config downloads,
image URLs, disconnected ZIP/device sources, or automatically replicate commits.
The ``url`` field remains a string; use ``fallbackURLs`` rather than changing it
to a YAML list.
