Purpose
=======

FetchIt provides Git-driven reconciliation for hosts running Podman without a
Kubernetes cluster. A repository describes the desired containers, Pods, host
services, or files, and the engine checks its configured branch on a cron schedule.

Choose Raw for Podman container definitions, Kube for Podman kube play manifests,
Quadlet for host-generated Podman/systemd services, Systemd for authored service
files, FileTransfer for host files, or Ansible for playbooks. See :doc:`methods`
for configuration and :doc:`samples` for runnable amd64/arm64 examples.

How it works
------------

The engine runs as a binary or container and connects to a host Podman socket.
It clones repositories, compares Git revisions, and applies changes through the
selected methods. Workloads run on that host; FetchIt is not a Kubernetes cluster
controller and does not provide cluster networking or scheduling.

Rootful and rootless operation use separate Podman instances. Quadlet and Systemd
also use the matching host systemd manager. See :doc:`running` for socket and
engine setup, and :doc:`quadlet` for explicit rootless host paths.

Configuration can reload without restarting the process. Optional repository
mirrors support source failover, SOPS protects selected Kube manifests in Git,
and the status endpoint reports liveness and scheduled activity. See
:doc:`mirrors`, :doc:`sops`, and :doc:`status`.

Method-removal cleanup and failed-apply rollback are opt-in. They have distinct
ownership and recovery requirements, and do not undo arbitrary persistent-data
writes or Ansible changes. Preserve the engine volume and host journals and read
:doc:`lifecycle` before enabling them on existing deployments.
