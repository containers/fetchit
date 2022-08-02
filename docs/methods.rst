Configuration
=============
The YAML configuration file defines git targets and the methods to use, how frequently to check the repository,
and various configuration values that relate to that method.

A target is a unique value that holds methods. Mutiple git targets (targetConfigs) can be defined. Methods that can be configured
include `Raw`, `Systemd`, `Kube`, `Ansible`, `FileTransfer`, `Prune`, and `ConfigReload`.

Dynamic Configuration Reload
=============

There are a few ways currently to trigger FetchIt to reload its targets without requiring a restart. The first is to
pass the environment variable `$FETCHIT_CONFIG_URL` to the `podman run` command running the FetchIt image.
The second is to include a ConfigReload. If neither of these exist, a restart of the FetchIt
pod is required to reload targetConfigs. The following fields are required with the ConfigReload method:

.. code-block:: yaml

   configReload:
     schedule: "*/5 * * * *"
     configUrl: https://raw.githubusercontent.com/sallyom/fetchit-config/main/config.yaml

Changes pushed to the ConfigURL will trigger a reloading of FetchIt target configs. It's recommended to include the ConfigReload
in the FetchIt config to enable updates to target configs without requiring a restart.

Methods
=======
Various methods are available to lifecycle and manage the container environment on a host. Funcionality also exists to
allow for files or directories of files to be deployed to the container host to be used by containers.


Ansible
-------
The AnsibleTarget method allows for an Ansible playbook to be run on the host. A container is created containing the Ansible playbook, and the container will run the playbook. This playbook can be used to install software, configure the host, or perform other tasks.
In the examples directory, there is an Ansible playbook that is used to install zsh.

.. code-block:: yaml

   targetConfigs:
   - url: http://github.com/containers/fetchit
     branch: main
     ansible:
     - name: ans-ex
       targetPath: examples/ansible
       sshDirectory: /root/.ssh
       schedule: "*/5 * * * *"

The field sshDirectory is unique for this method. This directory should contain the private key used to connect to the host and the public key should be copied into the `.ssh/authorized_keys` file to allow for connectivity. The .ssh directory should be owned by root.

Raw
---
The RawTarget method will launch containers based upon their definition in a JSON file. This method is the equivalent of using the `podman run` command on the host. Multiple JSON files can be defined within a directory.

.. code-block:: yaml

   targetConfigs:
   - url: http://github.com/containers/fetchit
     branch: main
     raw:
     - name: raw-ex
       targetPath: examples/raw
       schedule: "*/5 * * * *"
       pullImage: true

The pullImage field is useful if a container image uses the latest tag. This will ensure that the method will attempt to pull the container image every time.

A Raw JSON file can contain the following fields.

.. code-block:: json

   {
    "Image":"docker.io/mmumshad/simple-webapp-color:latest",
    "Name": "colors1",
    "Env": {"APP_COLOR": "pink", "tree": "trunk"},
    "Mounts": "",
    "Volumes": "",
    "Ports": [{
        "host_ip":        "",
        "container_port": 8080,
        "host_port":      8080,
        "range":         0,
        "protocol":      ""}]
   }

Volume and host mounts can be provided in the JSON file.

PodmanAutoUpdate
-------
If this method is present in the config file, podman-auto-update.service & podman-auto-update.timer
will be enabled on the host. Podman auto-update will look for image updates with all podman-generated unit files
that include the auto-update label, according to the timer schedule. Can configure for root, non-root, or both.

.. code-block:: yaml

   podmanAutoUpdate:
     root: true
     user: true

Systemd
-------
SystemdTarget is a method that will place, enable, and restart systemd unit files.

.. code-block:: yaml

   targetConfigs:
   - url: http://github.com/containers/fetchit
     branch: main
     systemd:
     - name: sysd-ex
       targetPath: examples/systemd
       root: true
       enable: true
       schedule: "*/5 * * * *"

File Transfer
-------------
The File Transfer method will copy files from the container to the host. This method is useful for transferring files from the container to the host to be used by the container either at start up or during runtime.

.. code-block:: yaml

   targetConfigs:
   - url: http://github.com/containers/fetchit
     filetransfer:
     - name: ft-ex
       targetPath: examples/filetransfer
       destinationDirectory: /tmp/ft
       schedule: "*/5 * * * *"
     branch: main

The destinationDirectory field is the directory on the host where the files will be copied to.

Kube Play
---------
The KubeTarget method will launch a container based upon a Kubernetes pod manifest. This is useful for launching containers to run the same way as they would in a Kubernetes environment.

.. code-block:: yaml

   targetConfigs:
   - url: http://github.com/containers/fetchit
     kube:
     - name: kube-ex
       targetPath: examples/kube
       schedule: "*/5 * * * *"
     branch: main

An example Kube play YAML file will look similiar to the following. This will launch a container as well as the coresponding ConfigMap.

.. code-block:: yaml

   apiVersion: v1
   kind: ConfigMap
   metadata:
   name: env
   data:
   APP_COLOR: red 
   tree: trunk
   ---
   apiVersion: v1
   kind: Pod
   metadata:
   name: colors_pod
   spec:
   containers:
   - name: colors-kubeplay
      image: docker.io/mmumshad/simple-webapp-color:latest
      ports:
      - containerPort: 8080
         hostPort: 7080
      envFrom:
      - configMapRef:
         name: env
         optional: false
