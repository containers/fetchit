Named volumes
=============

Named Podman volumes persist independently of workloads. FetchIt reuses existing
volumes and retains them when workloads are updated, removed from Git, or removed
through ``cleanupOnRemoval``. Creation options never reset or delete data.
Back up persistent data independently; Git rollback cannot restore volume contents.

Volume creation is opt-in per workload declaration or raw mount. There is no
method-wide automatic mount. Other workloads receive only their own declared
mounts. Podman itself may create volumes through its existing native behavior;
leaving the new creation option unset does not disable that behavior.

These options require a FetchIt engine built with the named-volume feature.
Volumes belong to the Podman store used by the workload: rootful and rootless
stores are separate, including stores belonging to different host users.

Kube
----

For a Pod that references named PVCs but does not include their declarations,
set ``fetchit.containers.io/create-volumes: "true"`` in its metadata annotations.
FetchIt adds missing PVC declarations to the in-memory input passed to Podman.
The repository file is not changed. The flag must be a quoted string; it defaults
to disabled. For Deployment, DaemonSet, and Job workloads, put the annotation
under ``spec.template.metadata.annotations``.

.. code-block:: yaml

   apiVersion: v1
   kind: Pod
   metadata:
     name: volume-example
     annotations:
       fetchit.containers.io/create-volumes: "true"
   spec:
     containers:
     - name: app
       image: docker.io/library/alpine:3.22
       command: [sleep, infinity]
       volumeMounts:
       - name: storage
         mountPath: /data
     volumes:
     - name: storage
       persistentVolumeClaim:
         claimName: example-data

Here ``example-data`` is the Podman volume name and ``/data`` is the mount path
in ``app``. Define ``volumeMounts`` separately for each container or init
container that needs access. Set ``readOnly: true`` on a container's mount when
appropriate. The option does not create storage for ``hostPath``, ``emptyDir``,
Secrets, or ConfigMaps.

Explicit PVC declarations in the same manifest take precedence over generated
ones. Include an explicit PVC when you need Podman driver, UID/GID, or other
volume annotations. When splitting files, include the PVC declaration with the
workload that references it; generated declarations cannot discover settings in
other files or methods. A missing volume uses Podman's local-driver defaults.
Existing volumes retain their driver, ownership, options, and contents. Sharing
a claim name shares storage; use different names when workloads need isolation.

Podman maps PVC names to named volumes; Kubernetes StorageClasses, CSI
provisioning, requested capacity quotas, and access-mode enforcement are not
provided by this feature. See the
`Podman kube play manual <https://docs.podman.io/en/v5.8.0/markdown/podman-kube-play.1.html>`_.

The same annotation works in SOPS-encrypted kube manifests. Generation occurs
after authenticated decryption, in memory, before workload application.

Raw volume mounts
-----------------

Use the existing ``Volumes`` list to mount named volumes and set ``create: true``
only on mounts that need explicit creation:

.. code-block:: yaml

   Image: docker.io/library/alpine:3.22
   Name: volume-example
   Volumes:
   - name: example-data
     dest: /data
     create: true
     options: [rw]
   - name: shared-reference
     dest: /shared
     options: [ro]

FetchIt ensures ``example-data`` exists before replacing the container, while
``shared-reference`` follows the existing Podman mount behavior. JSON accepts
the same lowercase keys inside ``Volumes``. ``create`` defaults to false and
requires a valid Podman volume name and an absolute container destination.
Repeated mounts of the same volume create it only once. Inspection or creation
errors stop deployment; existing volumes are never modified. Mount options,
including SELinux relabeling options, remain explicit workload choices.

Systemd and Quadlet volumes
---------------------------

These methods already support native volume creation and mounting. In a systemd
service's ``podman run`` command, use ``--volume example-data:/data``: Podman
creates a missing named volume. In Quadlet, declare a ``.volume`` unit and
reference it from a ``.container`` unit with ``Volume=data.volume:/data``.
Select the complete Quadlet bundle so the generator can resolve dependencies.
See :doc:`quadlet` and the
`Podman Quadlet manual <https://docs.podman.io/en/v5.8.0/markdown/podman-systemd.unit.5.html>`_.

FetchIt does not add mounts to arbitrary systemd services or to its file-transfer,
image, mirror, or configuration helpers. Set volume and mount requirements in
each workload's own definition.

Validation
----------

The GitHub workload-lifecycle workflow runs named-volume tests on Ubuntu rootful,
Ubuntu rootless, and Fedora with Podman 5. Tests verify mounting, retained data
on reapply, retained volumes after removal, and successful re-addition for raw,
generated kube PVCs, and explicit kube PVCs. Unit tests also cover mixed opt-in
and default declarations, invalid flags/names, existing PVC options, and failures
while inspecting volumes.
