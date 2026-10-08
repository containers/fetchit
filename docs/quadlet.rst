Quadlet
=======

The ``quadlet`` method deploys a complete Git directory into the host's Quadlet
search path. The host's installed Podman generator creates systemd services.
FetchIt validates the bundle before changing live files, reloads systemd once per
batch, and optionally starts or restarts workloads.

Use Podman **5.7 or newer within major version 5**, cgroup v2, and a running host
systemd manager. The helper uses the host's generator and binaries, so it validates
against the installed Podman rather than FetchIt's Go library version. Podman 6
is not supported by this method.

Quadlet configuration
---------------------

.. code-block:: yaml

   targetConfigs:
     - name: application
       url: https://github.com/your-org/application.git
       branch: main
       quadlet:
         - name: web
           schedule: "*/1 * * * *"
           targetPath: deploy/quadlet
           root: true
           start: true
           restart: true

``name`` and ``schedule`` follow the normal method configuration. ``targetPath``
must select a dedicated directory containing the complete bundle. Quadlet source
units belong at the bundle root; supporting files and drop-ins may be nested.
Bundles are limited to 32 MiB of file contents and 4,096 files; exceeding a limit
returns an error before allocating the file contents or starting a helper. Keep
large build contexts in a separate build pipeline or reduce the selected bundle.

``glob`` is deliberately unsupported because filtering can omit dependencies.
Symlinks, uninstantiated templates, and duplicate generated service names are
rejected. Concrete instance files such as ``web@blue.container`` are allowed.

``root`` defaults to false. ``start`` defaults to false and starts desired services
after deployment. ``restart`` defaults to false and implies ``start``; it restarts
workload services whenever the selected bundle changes, including supporting-file
changes. Network and volume units are started, rather than restarted, to preserve
resources used by running containers. Changing the configuration of an existing
network or volume may require a new resource/unit name and an explicit migration;
FetchIt does not migrate resource contents.

The generator recognizes ``.container``, ``.network``, ``.volume``, ``.pod``,
``.kube``, ``.image``, ``.build``, and ``.artifact``. FetchIt uses Podman's naming
helpers, including ``ServiceName=`` overrides and overrides in drop-ins. The
integration suite exercises container/network/volume bundles; test advanced
resource types and their inputs on your host before relying on them. FetchIt
copies all supporting files, including relative YAML, build contexts, environment
files, and ``.conf`` drop-ins. Absolute references still refer to host paths.

Boot activation comes from the source unit's ``[Install]`` section, for example
``WantedBy=multi-user.target`` for a system service or ``WantedBy=default.target``
for a user service. FetchIt does not run ``systemctl enable`` for generated units.
``start: false`` does not remove boot activation specified by ``[Install]``.

Host access and rootless operation
----------------------------------

FetchIt needs access to the host Podman socket. The Quadlet helper runs through
that socket with only the chroot, file access, ownership, and permission
capabilities needed for host operations; it does not request privileged mode. It mounts the host root
read-only to use the actual generator, libraries, and systemd private socket,
and mounts the configuration parent writable. It shares the host PID namespace
so systemd can authenticate the client. This is host administration
access; use trusted repositories and the existing commit-verification option
where appropriate. Files are installed into a method-owned subdirectory, not
into another method's directory. Rootful hosts must have ``/etc/containers``;
rootless hosts must have the configured configuration directory already created.

For rootless operation, use the intended user's Podman socket and provide their
host paths explicitly:

.. code-block:: yaml

   quadlet:
     - name: web
       schedule: "*/1 * * * *"
       targetPath: deploy/quadlet
       root: false
       hostHome: /home/operator
       hostConfigHome: /home/operator/.config
       hostRuntimeDir: /run/user/1234
       start: true
       restart: true

The host user manager must be running. Enable lingering for that user if workloads
must survive logout. FetchIt does not infer host paths from its container's UID,
``HOME``, or ``XDG_RUNTIME_DIR``. Rootless files are installed beneath
``hostConfigHome/containers/systemd``; system files beneath
``/etc/containers/systemd``.

``helperImage`` optionally selects the helper image. The default is
``quay.io/fetchit/fetchit:latest``. Use an image built from a FetchIt release with
Quadlet support: it must provide ``sh``, ``chroot``, ``flock``, ``cp``, ``find``,
``grep``, ``sed``, and ``sort``. For deployments using a registry, configure an immutable image digest in
``helperImage`` when available. Local development can use the image tag built from
the same checkout. Helper images and Git contents are trusted host administration
inputs; reducing container capabilities does not sandbox the Quadlet services.

New actions are carried in the FetchIt binary,
so no changes to the legacy Systemd helper image are required.

Updates, removal, and recovery
------------------------------

FetchIt stops removed services before deleting their source files. Renaming a
file or changing ``ServiceName=`` retires the old service. It refuses to overwrite
a service owned outside the method's bundle. Removing the complete selected
Git directory removes its deployed files and stops its managed services.
By default, removing a method from FetchIt's configuration retains its services.
Set ``cleanupOnRemoval: true`` while the method is still configured to register
persistent cleanup intent; removing it then cleans its journal-owned bundle on
reload/startup. See :doc:`lifecycle` for receipts, retries, and retained resources.
Without opt-in, remove its source units first while the method is still scheduled.

Generator, copy, reload, and service failures are reported and leave the applied
Git commit unchanged. Persistent helper state tracks services from interrupted
applications so the next run can retry safely, including when the desired commit
changes. Application is not an atomic transaction. Git targets may opt into best-effort
``rollback`` when they contain only Raw, Kube, or Quadlet methods; it is disabled
by default. See :doc:`lifecycle` for limits. Network and volume resources may remain
on the host after their units are removed; FetchIt does not force-delete data.
On startup, FetchIt checks the host receipt for an interrupted application, even
when the saved Git revision already matches the desired revision. After success,
an unchanged Git commit is a no-op; this method does not continuously repair host drift.

See the `runnable example <https://github.com/containers/fetchit/tree/main/examples/quadlet>`_
and the `Podman 5.8 Quadlet manual <https://docs.podman.io/en/v5.8.0/markdown/podman-systemd.unit.5.html>`_.

Run the example from a checkout
-------------------------------

These commands assume Linux, Go with automatic toolchain downloads enabled, and
Podman 5.7--5.x. Fedora 44 is the supported Fedora baseline for the Go dependency
update; Ubuntu 26.04 runs the Quadlet lifecycle workflow. Run the example against
an image built from this checkout until a release containing Quadlet is published.

Install host dependencies and build the image:

.. code-block:: bash

   # Fedora:
   sudo dnf install -y podman crun git golang
   # Ubuntu alternative:
   # sudo apt-get update
   # sudo apt-get install -y podman crun git golang

   git clone https://github.com/containers/fetchit.git
   cd fetchit
   go mod vendor
   # Use sudo podman for a rootful deployment, ordinary podman for rootless.
   # Replace amd64 with arm64 on an ARM64 host.
   sudo podman build --build-arg ARCH=amd64 -t localhost/fetchit:quadlet .

Use ``examples/quadlet/quadlet.yaml`` as the configuration. Set
``helperImage: localhost/fetchit:quadlet`` on the method so the helper uses the same
local image. Set the repository URL and branch to the Git revision containing your
bundle. During development, use the feature branch containing the example; after
merge, ``main`` contains it. A published release can use the default helper image.

For rootful deployment:

.. code-block:: bash

   sudo systemctl enable --now podman.socket
   mkdir -p "$HOME/.fetchit-quadlet"
   cp examples/quadlet/quadlet.yaml "$HOME/.fetchit-quadlet/config.yaml"
   # Edit config.yaml: set helperImage and the appropriate Git branch.
   sudo podman run -d --name fetchit-quadlet \
     --security-opt label=disable \
     -v fetchit-quadlet-volume:/opt \
     -v "$HOME/.fetchit-quadlet/config.yaml:/opt/mount/config.yaml:ro" \
     -v /run/podman/podman.sock:/run/podman/podman.sock \
     localhost/fetchit:quadlet

For rootless deployment, build the image into the user's image store instead of
root's store, then configure ``root: false``, ``hostHome``, ``hostConfigHome``, and
``hostRuntimeDir`` with that host user's paths. Display the values with:

.. code-block:: bash

   printf 'hostHome: %s\nhostConfigHome: %s\nhostRuntimeDir: /run/user/%s\n' \
     "$HOME" "${XDG_CONFIG_HOME:-$HOME/.config}" "$(id -u)"
   mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}" "$HOME/.fetchit-quadlet"
   sudo loginctl enable-linger "$(id -un)"
   export XDG_RUNTIME_DIR="/run/user/$(id -u)"
   systemctl --user enable --now podman.socket
   podman build --build-arg ARCH=amd64 -t localhost/fetchit:quadlet .
   # Prepare ~/.fetchit-quadlet/config.yaml with the rootless fields above.
   podman run -d --name fetchit-quadlet \
     --security-opt label=disable \
     -v fetchit-quadlet-volume:/opt \
     -v "$HOME/.fetchit-quadlet/config.yaml:/opt/mount/config.yaml:ro" \
     -v "$XDG_RUNTIME_DIR/podman/podman.sock:/run/podman/podman.sock" \
     localhost/fetchit:quadlet

Set the example's ``WantedBy=default.target`` when running it as a user service.
The named FetchIt volume preserves Git repositories and applied-commit tags across
container recreation. Recreating FetchIt without that volume loses its Git cleanup
baseline; keep it for updates and retries. Rootful and rootless Podman image,
container, network, and volume stores are separate, so consistently use the same
mode for building, running, inspecting, and removing resources.

Inspect services and diagnose failures
--------------------------------------

The generated default names are:

.. list-table:: Generated service names
   :header-rows: 1

   * - Source
     - Service
   * - ``web.container`` / ``web.kube``
     - ``web.service``
   * - ``web.network``
     - ``web-network.service``
   * - ``web.volume``
     - ``web-volume.service``
   * - ``web.pod``
     - ``web-pod.service``
   * - ``web.image``
     - ``web-image.service``
   * - ``web.build``
     - ``web-build.service``
   * - ``web.artifact``
     - ``web-artifact.service``

``ServiceName=`` overrides these names and omits the ``.service`` suffix in the
source value. Drop-ins with the same filename follow Podman's specificity rules;
selected drop-ins are merged alphabetically. The generator manages dependencies
such as ``Network=web.network`` and ``Volume=web.volume:/data``.

For the rootful example:

.. code-block:: bash

   sudo podman logs fetchit-quadlet
   sudo systemctl status web.service
   sudo systemctl cat web.service
   sudo systemctl show web.service --property=SourcePath
   sudo journalctl -u web.service --no-pager -n 100
   sudo podman exec systemd-web cat /data/message

For rootless operation, use ordinary ``podman`` and add ``--user`` to the
``systemctl`` and ``journalctl`` commands. ``SourcePath`` identifies the managed
Quadlet subdirectory, which also contains supporting files. The helper writes
``validation.log`` in a sibling hidden control directory beneath the configured
parent. FetchIt logs contain generator output when validation fails and the
helper's exit status when a host command fails.

If a service is not generated, inspect the validation log for missing required
keys or options unsupported by the host's Podman version. If startup fails, inspect
the service journal for image-pull errors, unavailable host paths, or a failed
``ExecStartPre``. Fix the desired Git bundle or the missing host prerequisite; the
same commit is retried on the next schedule because it was not recorded as applied.
For user-manager connection failures, confirm that the configured host home and
runtime paths belong to the socket's user and that their user manager is running.
An ownership error means another bundle or regular systemd service already uses
the generated name; choose a distinct ``ServiceName=``.

To update workloads, push changes to the selected Git directory and wait for the
configured schedule. To remove workloads, delete their Quadlet sources in Git and
leave FetchIt running until it applies the removal. Only then remove the FetchIt
container or method configuration. Network/volume data retention and configuration
reload behavior follow the earlier sections; deleting the FetchIt container alone
does not stop its host-managed services.

Configuration changes and concurrent controllers
------------------------------------------------

Applied-commit tags include the bundle path, branch, host paths, and activation
settings. Changing ``start``, ``restart``, or ``helperImage`` therefore triggers
application on configuration reload, even if the Git commit is unchanged. The
host directory identity stays stable for activation changes, preserving ownership.
Changing the root/user scope, host paths, branch, or bundle path changes ownership.
With cleanup enabled, the old identity is retired and cleaned up. Without it,
remove the old sources while their previous method is still configured. FetchIt
does not migrate resource contents to a different manager or path. Trailing
slashes in ``targetPath`` do not change ownership.
The existing :doc:`methods` configuration-reload mechanism also applies to Quadlet.

Use one active configuration owner for each bundle. The host lock serializes helper
operations, and its applied receipt rejects a stale plan whose expected revision
no longer matches the host. A second controller with the same configuration should
refresh its applied Git state before retrying a stale-plan error. Do not deliberately
run competing controllers with different settings for the same managed bundle.
Helper-container removal errors also fail application; inspect stopped helper
containers with ``podman ps -a`` and resolve the removal error before retrying.
