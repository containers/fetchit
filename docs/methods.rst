Configuration
=============
The YAML configuration file defines the method to use, how frequently to check the repository, and various configuration values that relate to that method.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   raw:
      targetPath: examples/raw
      schedule: "*/5 * * * *"
      pullImage: true
   branch: main

A target is a unique value but the methods within each target cannot be repeated. Mutiple targets can be defined.

Methods
=======
Various methods are available to lifecycle and manage the container environment on a host. Funcionality also exists to allow for files or directories of files to be deployed to the container host to be used by containers.


Ansible
-------
The Ansible method allows for an Ansible playbook to be run on the host. A container is created containing the Ansible playbook, and the container will run the playbook. This playbook can be used to install software, configure the host, or perform other tasks.
In the examples directory, there is an Ansible playbook that is used to install zsh.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   ansible: 
      targetPath: examples/ansible
      sshDirectory: /root/.ssh
      schedule: "*/5 * * * *"
   branch: main

The field sshDirectory is unique for this method. This directory should contain the private key used to connect to the host and the public key should be copied into the `.ssh/authorized_keys` file to allow for connectivity. The .ssh directory should be owned by root.

Raw
---
The Raw method will launch containers based upon their definition in a JSON file. This method is the equivalent of using the `podman run` command on the host. Multiple JSON files can be defined within a directory.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   raw:
      targetPath: examples/raw
      schedule: "*/5 * * * *"
      pullImage: true
   branch: main

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


Systemd
-------
Systemd is a method that will create a systemd unit file. In the future this method will also start or update the unit. This method is useful for creating services that can be started and stopped.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   systemd:
      targetPath: examples/systemd
      root: true
      enable: true
      schedule: "*/5 * * * *"
   branch: main

File Transfer
-------------
The File Transfer method will copy files from the container to the host. This method is useful for transferring files from the container to the host to be used by the container either at start up or during runtime.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   filetransfer:
      targetPath: examples/filetransfer
      destinationDirectory: /tmp/ft
      schedule: "*/5 * * * *"
   branch: main

The destinationDirectory field is the directory on the host where the files will be copied to.

Kube Play
---------
The Kube play method will launch a container based upon a Kubernetes pod manifest. This is useful for launching containers to run the same way as they would in a Kubernetes environment.

.. code-block:: yaml

   volume: harpoon-volume
   targets:
   - name: harpoon
   url: http://github.com/redhat-et/harpoon
   kube: 
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
