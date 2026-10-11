One root configuration per host
===============================

FetchIt already supports a root configuration that defines all of a host's
workloads. This provides the entry-point pattern requested in
`issue #417 <https://github.com/containers/fetchit/issues/417>`_: point each host
at one FetchIt YAML file in Git, then let that file define multiple independently
scheduled methods, possibly drawing workloads from different repositories.
The root can update its own method configuration through ``configReload``.
No additional FetchIt instance or Kubernetes controller is needed.

This is analogous to the root entry point of an app-of-apps deployment. FetchIt
uses its own ``targetConfigs`` and method schema. It does **not** recursively
import child FetchIt YAML files or implement Argo CD Application resources,
ApplicationSets, dependency ordering, or transactional workload rollout.
Each method reconciles independently. Configuration composition is a separate
feature; the existing root workflow does not require it.

Repository layout and root file
-------------------------------

For example, keep one root per host or host group in a configuration repository:

.. code-block:: text

   fleet-config/
     hosts/
       edge-01.yaml
       edge-02.yaml

``hosts/edge-01.yaml`` can reference separate workload repositories:

.. code-block:: yaml

   configReload:
     configURL: https://raw.githubusercontent.com/example/fleet-config/main/hosts/edge-01.yaml
     schedule: "*/5 * * * *"
   targetConfigs:
     - name: frontend-source
       url: https://github.com/example/frontend-workloads.git
       branch: main
       raw:
         - name: frontend
           targetPath: deploy/raw
           schedule: "*/1 * * * *"
     - name: monitoring-source
       url: https://github.com/example/monitoring-workloads.git
       branch: main
       quadlet:
         - name: monitoring
           targetPath: deploy/quadlet
           root: true
           schedule: "*/1 * * * *"

Replace the URLs with your repositories and ensure the selected paths contain
valid workload definitions for those methods. The first target applies raw
Podman definitions; the second applies a Quadlet bundle. Other documented
methods can be combined in the same root. See :doc:`methods` for each method's
schema and prerequisites. Method names must remain stable to retain their Git
baselines and lifecycle identities.

The root is downloaded using HTTP(S), such as a Git hosting service's **raw file
URL**, rather than cloned using Git. ``configURL`` is not a Git repository URL
or an SSH clone URL. Workload ``url`` fields, by contrast, identify repositories
that FetchIt clones/fetches on the configured branch. A root file can reference
several methods within one repository or methods across several repositories.

Bootstrap without a local configuration
---------------------------------------

For a public root, start with an empty, writable mount directory:

.. code-block:: bash

   mkdir -p "$HOME/fetchit-config"
   chmod 700 "$HOME/fetchit-config"
   sudo podman run -d --name fetchit --security-opt label=disable \
     -e FETCHIT_CONFIG_URL=https://raw.githubusercontent.com/example/fleet-config/main/hosts/edge-01.yaml \
     -v fetchit-state:/opt \
     -v "$HOME/fetchit-config:/opt/mount:Z" \
     -v /run/podman/podman.sock:/run/podman/podman.sock \
     quay.io/fetchit/fetchit:latest

This example uses the rootful Podman socket; enable that socket before launch.
For rootless deployment, use the socket mounts and prerequisites in
:doc:`running`. The named volume retains repository baselines and lifecycle
state. The mount directory must be writable because FetchIt publishes
configuration updates with atomic rename; mounting an individual config file
or a read-only directory cannot support that operation.

An existing local ``/opt/mount/config.yaml`` takes precedence on initial startup.
For the URL bootstrap above, ensure that file does not already exist. FetchIt
requires a valid root response before starting its methods. Supplying an
environment URL alone does not establish recurring reloads: keep a
``configReload`` block in the downloaded root, pointing to its own raw URL.
Choose a different entry-point URL for each host or host group. Automatically
reading cloud machine metadata is not implemented; provision the URL through
your existing service/container environment or bootstrap automation.

For private roots, follow the local bootstrap and authentication instructions
in :doc:`methods`. The initial URL request before a local configuration is
loaded has no configured credentials. Do not assume merely setting a token
environment variable authenticates that first request. Keep top-level
``gitAuth`` settings in the downloaded root as needed for subsequent downloads
and workload repositories; do not store credentials in Git or URLs.

Updating the root and its workloads
-----------------------------------

Commit a new method or target to the root file. On the next successful
``configReload`` check, FetchIt validates and publishes the new configuration,
then reloads the engine's targets **inside the existing process**. New methods
start and existing methods replay/reconcile their saved Git baselines. The
FetchIt container does not need to be replaced or restarted.

Changes to workload files are independently observed by the methods' Git
schedules. A workload commit does not require changing the root YAML. Updating
the root and workload files together does not make their deployment atomic:
methods have separate schedules, and application failures can leave a partial
workload rollout. Removing a method retains its workload by default. Use
``cleanupOnRemoval`` only where supported and intentionally enabled; see
:doc:`lifecycle` for ownership, cleanup, and rollback behavior.

A failed root download or invalid root schema preserves the active configuration
and backup. A valid root is not proof that every workload will apply: image
availability, repository access, and method runtime requirements are checked
later. Monitor logs and the :doc:`status` endpoint for invocation activity.
See :doc:`running` for remote configuration validation and failure behavior.

Security and trust
------------------

A root configuration controls host operations through FetchIt's Podman socket.
Treat access to its repository as administrative authority over the managed
host. Use a trusted repository, restricted write access, reviewed changes, and
HTTPS with certificate verification. Do not serve the root over an untrusted
plain HTTP network. FetchIt uses ordinary HTTP(S) certificate verification for
these downloads; workload ``gitsignVerify`` settings do not authenticate the
root file itself. A raw URL pinned to a reviewed commit can give an immutable
root revision, but automatic root updates then require updating that URL.

Keep secret material outside root/workload repositories. All targets use the
root's configured Git authentication; separate per-child credentials and
privilege isolation are not part of this pattern. Do not give untrusted teams
root repository access merely to delegate a workload. A writable configuration
mount is sensitive local state and should be readable only by its intended
owner. Root download validation limits document size and rejects malformed or
unknown fields; it does not turn an untrusted configuration into a safe one.

Verification
------------

The repository includes ``tests/integration/root-config/test-root-config.sh``.
It uses a private, loopback-only Git fixture and the production engine image to:

* Start with no local configuration and download a root YAML served from Git.
* Apply two separately configured methods from that root.
* Commit an expanded root that adds a third method and updates an existing
  workload, without restarting the engine container.
* Reject an invalid root update while preserving the active config, backup
  history, and existing workloads.

Run it with rootful Podman, the socket enabled, and the engine/sample images
available:

.. code-block:: bash

   sudo env FETCHIT_TEST_ENGINE_IMAGE=localhost/fetchit:test \
     bash tests/integration/root-config/test-root-config.sh

The fixture's plain HTTP transport is confined to host loopback for testing.
The test cleans up its own containers, state volume, Git server, and temporary
files. It does not push to GitHub or query cloud instance metadata.
