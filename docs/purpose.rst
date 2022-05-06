Purpose
=======

With the adoption of GitOps tools such as ArgoCD and Red Hat Advanced Cluster Management for Kubernetes(RHACM) a very mature set of tools have been established allowing for the lifecycle management of containers running on Kubernetes. A technical gap exists for those environments running containers on the host without Kubernetes. Tools like Ansible can deploy updates to a container and lifecycle the application but this requires a playbook to be dynamically generated and ran on the host through GitHub actions or using tools such as Ansible Automation Platform. However, these tools do not offer a solution that matches the features and functionality of ArgoCD and RHACM because a host or localhost is required to act as a intermediary between the git repository and the host in which the containers are running.

This gap provides the opportunity for a tool to be written specifically for the management of a container's lifecycle. The purpose of the FetchIt project is to allow for the definition of a container(s) that defines the required ports, volumes, environment variables, and mounts. This definition can be updated and submitted to a git repository. FetchIt will notice that changes to the container are required based on the git commit history and deploy the new container as it is defined in git.

Why FetchIt?
------------

FetchIt allows for a GitOps based approach to manage containers running on a single host or multiple hosts based on a git repository. This allows for us to deploy a new host and provide the host a configuration value for FetchIt and automatically any containers defined in the git repository and branch will be deployed onto the host. This can be beneficial for environments that do not require the complexity of Kubernetes to manage the containers running on the host.

The lifecycle of containers can and should be an automated process. Because changes to a containerized application can occur many times a day the process to modify a running container or deploy new containers should automatically occur.

How
---

FetchIt operates by running a binary or in a container on a host. FetchIt is given a configuration file which defines the way a container is deployed(engine), a repository, a specific branch within the repository, and additional variables used by the engine to lifecycle a container(s). Within the configuration file also exists a schedule based upon cron specifications. This schedule tells FetchIt when and how frequently to perform a git pull of a repository to look for changed objects.

The FetchIt engine defines a specific method and specification to deploy a container. For example, we would want a specific process to deploy a container(s) based on the Podman pod specification. Various engines will be created to allow for the lifecycle management of containers regardless of the process in which they were deployed on the host.

Podman allows for the usage of a socket to deploy, stop, and remove containers. This socket can be enabled for users which allows for FetchIt to run without the need for privilege escalation. 
