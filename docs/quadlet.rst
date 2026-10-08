Quadlet
=======

The ``quadlet`` method deploys a complete Git directory into the host's Quadlet
search path. The host's installed Podman generator creates systemd services.
Fetchit validates the bundle before changing live files, reloads systemd once per
batch, and optionally starts or restarts workloads.

Use Podman **5.7 or newer within major version 5**, cgroup v2, and a running host
systemd manager. The helper uses the host's generator and binaries, so it validates
against the installed Podman rather than Fetchit's Go library version. Podman 6
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
``glob`` is deliberately unsupported because filtering can omit dependencies.
Symlinks, uninstantiated templates, and duplicate generated service names are
rejected. Concrete instance files such as ``web@blue.container`` are allowed.

``root`` defaults to false. ``start`` defaults to false and starts desired services
after deployment. ``restart`` defaults to false and implies ``start``; it restarts
workload services whenever the selected bundle changes, including supporting-file
changes. Network and volume units are started, rather than restarted, to preserve
resources used by running containers. Changing the configuration of an existing
network or volume may require a new resource/unit name and an explicit migration;
Fetchit does not migrate resource contents.

The generator recognizes ``.container``, ``.network``, ``.volume``, ``.pod``,
``.kube``, ``.image``, ``.build``, and ``.artifact``. Fetchit uses Podman's naming
helpers, including ``ServiceName=`` overrides and overrides in drop-ins. The
integration suite exercises container/network/volume bundles; test advanced
resource types and their inputs on your host before relying on them. Fetchit
copies all supporting files, including relative YAML, build contexts, environment
files, and ``.conf`` drop-ins. Absolute references still refer to host paths.

Boot activation comes from the source unit's ``[Install]`` section, for example
``WantedBy=multi-user.target`` for a system service or ``WantedBy=default.target``
for a user service. Fetchit does not run ``systemctl enable`` for generated units.
``start: false`` does not remove boot activation specified by ``[Install]``.

Host access and rootless operation
----------------------------------

Fetchit needs access to the host Podman socket. The Quadlet helper runs through
that socket with privileged container permissions. It mounts the host root
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
must survive logout. Fetchit does not infer host paths from its container's UID,
``HOME``, or ``XDG_RUNTIME_DIR``. Rootless files are installed beneath
``hostConfigHome/containers/systemd``; system files beneath
``/etc/containers/systemd``.

``helperImage`` optionally selects the helper image. The default is
``quay.io/fetchit/fetchit:latest``. Use an image built from a Fetchit release with
Quadlet support: it must provide ``sh``, ``chroot``, ``flock``, ``cp``, ``find``,
``grep``, ``sed``, and ``sort``. New actions are carried in the Fetchit binary,
so no changes to the legacy Systemd helper image are required.

Updates, removal, and recovery
------------------------------

Fetchit stops removed services before deleting their source files. Renaming a
file or changing ``ServiceName=`` retires the old service. It refuses to overwrite
a service owned outside the method's bundle. Removing the complete selected
Git directory removes its deployed files and stops its managed services.
Removing a method from Fetchit's configuration does not perform this cleanup;
remove its source units first while the method is still scheduled.

Generator, copy, reload, and service failures are reported and leave the applied
Git commit unchanged. Persistent helper state tracks services from interrupted
applications so the next run can retry safely, including when the desired commit
changes. Application is not an atomic transaction and does not automatically
roll back a partially applied batch. Network and volume resources may remain
on the host after their units are removed; Fetchit does not force-delete data.
An unchanged Git commit is a no-op; this method does not continuously repair host drift.

See the `runnable example <https://github.com/containers/fetchit/tree/main/examples/quadlet>`_
and the `Podman 5.8 Quadlet manual <https://docs.podman.io/en/v5.8.0/markdown/podman-systemd.unit.5.html>`_.
