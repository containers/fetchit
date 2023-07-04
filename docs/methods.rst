Configuration
=============
The YAML configuration file defines git targets and the methods to use, how frequently to check the repository,
and various configuration values that relate to that method.

A target is a unique value that holds methods. Mutiple git targets (targetConfigs) can be defined. Methods that can be configured
include `Raw`, `Systemd`, `Kube`, `Ansible`, `FileTransfer`, `Prune`, and `ConfigReload`.

Examples of all methods are located in the `FetchIt repository <https://github.com/containers/fetchit/tree/main/examples>`_

Dynamic Configuration Reload
----------------------------

There are a few ways currently to trigger FetchIt to reload its targets without requiring a restart. The first is to
pass the environment variable `$FETCHIT_CONFIG_URL` to the `podman run` command running the FetchIt image.
The second is to include a ConfigReload. If neither of these exist, a restart of the FetchIt
pod is required to reload targetConfigs. The following fields are required with the ConfigReload method:

.. code-block:: yaml

   configReload:
     schedule: "*/5 * * * *"
     configUrl: https://raw.githubusercontent.com/containers/fetchit/main/examples/config-reload.yaml

Changes pushed to the ConfigURL will trigger a reloading of FetchIt target configs. It's recommended to include the ConfigReload
in the FetchIt config to enable updates to target configs without requiring a restart.

The configuration above will pull in the file from the repository and reload the FetchIt config. 
The YAML above demonstrates the minimal required objects to start FetchIt. Once FetchIt is running, the full configuration file 
that is stored in git will be used.

Dynamic Configuration Reload Using a Private Registry
-----------------------------------------------------

The ConfigReload method can be used to reload target configs from a private registry but this comes with the warning to ensure that
the repository is not public. The config.yaml will need to include the credentials to access the private registry.

When using a GitHub PAT token, the config.yaml will need to include the following fields:

.. code-block:: yaml

   configReload:
     schedule: "*/5 * * * *"
     pat: github-alphanumeric-token
     configUrl: https://raw.githubusercontent.com/containers/fetchit/main/examples/config-reload.yaml

When using basic authentication the config.yaml will need to include the following fields:

.. code-block:: yaml

  gitAuth:
    username: bob
    password: bobpassword
   configReload:
     schedule: "*/5 * * * *"
     configUrl: https://raw.githubusercontent.com/containers/fetchit/main/examples/config-reload.yaml

NOTE: This is not recommended for public repositories. As your credentials will need to be in clear text in the config.yaml.

PAT is the preferred method of authentication when available as the credentials can be reissued or locked. The PAT will be used both for the configuration file and the repo

.. code-block:: yaml

    gitAuth:
      pat: github-alphanumeric-token
   configReload:
     schedule: "*/5 * * * *"
     configUrl: https://raw.githubusercontent.com/containers/fetchit/main/examples/config-reload.yaml


Configuring FetchIt Using Environment Variables
-----------------------------------------------

FetchIt can also be configured by providing the FetchIt config through the `FETCHIT_CONFIG` environment variable. 
This approach will use the contents of `FETCHIT_CONFIG` to configure the FetchIt application.
This variable takes precedence over the FetchIt config file and will overwrite its contents if both are provided. 

Methods
=======
Various methods are available to lifecycle and manage the container environment on a host. Funcionality also exists to
allow for files or directories of files to be deployed to the container host to be used by containers.


All methods are defined within specific targetConfiguration sections. These sections are demonstrated below. For private repositories, a PAT token or a username/password combination is required.

An example of using a PAT token is shown below.

.. code-block:: yaml

   gitAuth:
     pat: CHANGEME
   targetConfigs:
   - url: https://github.com/containers/fetchit
     branch: main
     raw:
     - name: raw-ex
       targetPath: examples/raw
       schedule: "*/5 * * * *"
       pullImage: true

A SSH key can also be used for the cloning of a repository. An example of using an SSH key is shown below.

NOTE: The key must be defined within your git provider to be able to be used for pulling.

.. code-block:: bash

   mkdir -p ~/.fetchit/.ssh
   cp -rp ~/.ssh/id_rsa ~/.fetchit/.ssh/id_rsa
   chmod 0600 -R ~/.fetchit/.ssh
   ssh-keyscan -t ecdsa github.com >> ~/.fetchit/.ssh/known_hosts
   ssh-keyscan -t rsa github.com > ~/.fetchit/.ssh/known_hosts


The configuration file to use the key is shown below.

.. code-block:: yaml

   gitAuth:
     ssh: true
     sshKeyFile: id_rsa
   targetConfigs:
   - url: git@github.com:containers/fetchit
     raw:
     - name: raw-ex
       targetPath: examples/raw
       schedule: "*/5 * * * *"
       pullImage: true


An example of using username/password is shown below.

.. code-block:: yaml

    gitAuth:
      username: bob
      password: bobpassword
   targetConfigs:
   - url: https://github.com/containers/fetchit
     branch: main
     raw:
     - name: raw-ex
       targetPath: examples/raw
       schedule: "*/5 * * * *"
       pullImage: true

Podman secrets can also be used but FetchIt must be started with the secret defined as an environment variable.
This variable is defined as `--secret GH_PAT,type=env` in the `podman run` command.

.. code-block:: bash

   export GH_PAT_TOKEN=CHANGEME
   podman secret create --env GH_PAT GH_PAT_TOKEN 
   podman run -d --name fetchit     -v fetchit-volume:/opt     -v $HOME/.fetchit:/opt/mount     -v /run/user/1000/podman/podman.sock:/run/podman/podman.sock --secret GH_PAT,type=env --security-opt label=disable --secret GH_PAT,type=env quay.io/fetchit/fetchit:latest

Ansible
-------
The AnsibleTarget method allows for an Ansible playbook to be run on the host. A container is created containing the Ansible playbook, and the container will run the playbook. This playbook can be used to install software, configure the host, or perform other tasks.
In the examples directory, there is an Ansible playbook that is used to install zsh.

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/containers/fetchit
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
   - url: https://github.com/containers/fetchit
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
   - url: https://github.com/containers/fetchit
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
   - url: https://github.com/containers/fetchit
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
   - url: https://github.com/containers/fetchit
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
