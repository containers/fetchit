

Purpose
=======

Harpoon was designed to bring GitOps practices to Podman. 
Harpoon will maintain the lifecycle and manage the deployment as well as the updating of containers running on a system.

Why Harpoon?
------------
GitOps practices within Kubernetes have been proven to allow for the management of applications. In some circumstances a full Kubernetes deployment is not required. Harpoon looks to bring the capabilies of container management through the monitoring of a Git repository.

How
---
Harpoon operates by running a container on the operating system. This container monitors a file defining the repository containing the deployment mechanism and the method used to deploy the container.
