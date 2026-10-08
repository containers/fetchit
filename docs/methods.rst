Configuration
=============
The YAML configuration file defines git targets and the methods to use, how frequently to check the repository,
and various configuration values that relate to that method.

A target is a unique value that holds methods. Mutiple git targets (targetConfigs) can be defined. Methods that can be configured
include `Raw`, `Systemd`, `Quadlet`, `Kube`, `Ansible`, `FileTransfer`, `Prune`, and `ConfigReload`.

Examples of all methods are located in the `FetchIt repository <https://github.com/containers/fetchit/tree/main/examples>`_

Git target paths
----------------

A trailing ``/`` in ``targetPath`` is optional. For example, both configurations
below select the same Git directory and use the same method ownership identity:

.. code-block:: yaml

   targetPath: examples/filetransfer

.. code-block:: yaml

   targetPath: examples/filetransfer/

FetchIt removes trailing slashes before selecting Git changes and computing
method identities. Multiple trailing slashes are also accepted. Existing
configurations without a trailing slash continue to work. This does not change
absolute-path validation or normalize interior path components.

FileTransfer continues to copy each file by its basename into
``destinationDirectory``; adding a trailing slash does not preserve nested source
directories or change its destination layout.

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

For opt-in cleanup when a method leaves configuration, and recovery after a failed
Git apply, see :doc:`lifecycle`.

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
Methods manage containers, host services, and files. The examples below show the
configuration keys used inside ``targetConfigs``. Use a FetchIt image containing
the documented features; see :doc:`release_notes` for unreleased changes.

.. list-table:: Method selection
   :header-rows: 1
   :widths: 20 40 40

   * - Configuration key
     - Use it for
     - Removal behavior
   * - ``raw``
     - Podman container definitions in JSON/YAML
     - Optional owned-container cleanup
   * - ``kube``
     - Podman kube play manifests, optionally encrypted with SOPS
     - Optional owned-Pod cleanup
   * - ``quadlet``
     - Complete host-managed Podman/systemd bundles
     - Optional cleanup using the host bundle journal
   * - ``systemd``
     - Authored host ``.service`` files
     - Optional tracked-service cleanup
   * - ``filetransfer``
     - Copy files into an existing host directory
     - Optional tracked-file cleanup
   * - ``ansible``
     - Host configuration through playbooks
     - No automatic undo of playbook changes

``cleanupOnRemoval`` defaults to false. Removing a method from configuration
retains its resources unless cleanup was enabled beforehand. The method sections
and :doc:`lifecycle` explain ownership, migration, and recovery requirements.
Trailing slashes in ``targetPath`` are optional; the Git target paths section
above explains normalization and FileTransfer's flat destination layout.

See :doc:`samples` for current runnable applications, amd64/arm64 images, ports,
and archive-loading instructions. HTTP examples use container port 8080 and the
FetchIt sample image; ``APP_COLOR`` remains a configuration example and does not
change the page color.


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
   chmod 0700 ~/.fetchit/.ssh
   chmod 0600 ~/.fetchit/.ssh/id_rsa
   ssh-keyscan -t ed25519 github.com > /tmp/github.keys
   ssh-keygen -lf /tmp/github.keys -E sha256


Compare the printed SHA256 fingerprint with GitHub's `published SSH fingerprints
<https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints>`_
through a trusted channel. Only after they match, install the verified key:

.. code-block:: bash

   install -m 0600 /tmp/github.keys ~/.fetchit/.ssh/known_hosts

Mount the directory at ``/opt/mount/.ssh`` (normally
as part of the ``~/.fetchit:/opt/mount`` directory mount). An unknown or changed
host key fails authentication; do not disable host key checking. For a nonstandard
port, use ``ssh://git@host:2222/path/repo.git`` and a matching ``[host]:2222``
known-hosts entry. Set ``sshKeyFile`` to the private key filename, including an
Ed25519 key if desired. The SSH key is used for both clone and later fetches.

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
    "Image":"quay.io/fetchit/fetchit-sample-app:latest",
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

The sample HTTP image supports amd64 and arm64 and runs as user 1001 and listens on container port 8080 without capability overrides.
``APP_COLOR`` and ``tree`` illustrate environment propagation; Apache does not
change its welcome-page color based on these values. See :doc:`samples`.

PodmanAutoUpdate
----------------
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

Systemd also accepts ``cleanupOnRemoval: true`` to track deployed service files
and stop, disable, and remove its owned services when the method is removed.
The default is false. Rootless tracked deployments require the host user's
``HOME`` and ``XDG_RUNTIME_DIR`` in FetchIt's environment. See :doc:`lifecycle`
for prerequisites, existing-file migration, and recovery.

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

The ``destinationDirectory`` field is the directory on the host where files
will be copied. Set ``cleanupOnRemoval: true`` to track individual copied files
and remove them when the method leaves configuration. The default is false.
Tracked deployments require an existing, absolute destination directory and
refuse to overwrite untracked files. See :doc:`lifecycle` for setup and recovery.

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

Workload labels
~~~~~~~~~~~~~~~

FetchIt automatically adds the following labels to newly created or recreated Pods and to the
Pod templates of Deployments, DaemonSets, and Jobs. Podman 5 propagates these
labels to the resulting Pods and workload containers, including init containers:

* ``fetchit.containers.io/managed-by=fetchit``
* ``fetchit.containers.io/owner=<32-character hexadecimal identity>``

The owner identity is the first 16 bytes of a SHA-256 digest of the JSON array
containing the configured repository URL, branch, method name, and target path,
in that order. It is stable across FetchIt restarts and commit changes. Changing
one of those configuration fields changes the identity. The label contains no
literal repository URL or credentials; it is an identifier, not a security token.

Existing application labels and controller selectors are preserved. FetchIt
replaces values for its two reserved label keys. Labels are added in memory,
including after authenticated SOPS decryption; repository manifests are unchanged.
Secrets, ConfigMaps, and volumes do not receive these labels.

Use labels to find workloads managed by FetchIt:

.. code-block:: bash

   podman pod ps --filter label=fetchit.containers.io/managed-by=fetchit
   podman ps -a --filter label=fetchit.containers.io/managed-by=fetchit
   # Replace POD with a Pod name shown by the first command.
   owner_id=$(podman pod inspect POD --format '{{ index .Labels "fetchit.containers.io/owner" }}')
   podman ps -a --filter "label=fetchit.containers.io/owner=$owner_id"

This is an Unreleased feature; older images do not add these labels. Existing
workloads acquire labels on their next successful recreation by FetchIt. Kube startup
reconciliation may replay the saved applied revision and recreate workloads;
labels are added during that recreation.

Ordinary Git reconciliation retains its existing name-based replacement/deletion
behavior, including for unlabeled workloads. Opt-in removal of a configuration
method uses ownership labels; see :doc:`lifecycle` for that separate policy. Use unique resource names and avoid sharing workload
names between methods or with manually created workloads. Labels are editable metadata and are not a security boundary against other
users of the Podman socket.

Manifest example
~~~~~~~~~~~~~~~~

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
     image: quay.io/fetchit/fetchit-sample-app:latest
     ports:
     - containerPort: 8080
       hostPort: 7080
     envFrom:
     - configMapRef:
         name: env
         optional: false

Quadlet Method
--------------

The ``quadlet`` method installs a complete Git bundle into the host's Quadlet
search path. The host Podman generator validates the bundle and generates systemd
services. Use host Podman **5.7 or newer within major version 5**, cgroup v2, and a
running host systemd manager. Podman 6 is not supported by this method.

The checked-in example deploys a container, network, volume, environment file,
and drop-in:

.. code-block:: yaml

   targetConfigs:
   - name: quadlet-example
     url: https://github.com/containers/fetchit.git
     branch: main
     quadlet:
     - name: web
       targetPath: examples/quadlet/units/
       schedule: "*/1 * * * *"
       root: true
       start: true
       restart: true
       cleanupOnRemoval: true

The example writes a message into its named volume; it does not publish an HTTP
port. Select the complete units directory, not an individual file. Source units
belong at its root; supporting files and drop-ins may be nested. ``glob`` is
unsupported because it could omit dependencies. Bundles are limited to 32 MiB
and 4,096 files.

``root`` defaults to false. ``start`` defaults to false and starts services after
deployment. ``restart`` defaults to false and implies ``start``; changed supporting
files also trigger workload restarts. Network and volume services are started
rather than restarted. Boot activation comes from the units' ``[Install]``
sections, not ``systemctl enable`` on generated services.

For rootless operation, use the host user's Podman socket, running user manager,
and explicit host paths:

.. code-block:: yaml

   quadlet:
   - name: web
     targetPath: examples/quadlet/units
     schedule: "*/1 * * * *"
     root: false
     hostHome: /home/operator
     hostConfigHome: /home/operator/.config
     hostRuntimeDir: /run/user/1234
     start: true
     restart: true
     cleanupOnRemoval: true

Replace the home and runtime paths with those of the actual host user. These are
host paths, not paths inferred from FetchIt's container. Create the configured
host configuration directory and enable lingering if services must survive logout.
Rootful deployment requires ``/etc/containers`` on the host.

``helperImage`` optionally selects a compatible FetchIt helper; its default is
``quay.io/fetchit/fetchit:latest``. Engine and helper images must include Quadlet
support. The helper uses host binaries and administrative filesystem access, so
use trusted repositories and images.

``cleanupOnRemoval: true`` enables persisted bundle cleanup when the method leaves
configuration. Cleanup stops journal-owned services and removes its bundle;
volumes and other resources follow the authored units' stop behavior. Defaults
retain workloads. See :doc:`quadlet` for the full setup, supported unit types,
inspection commands, and migration limits, and :doc:`lifecycle` for retry and
rollback behavior.


Optional Podman networks
------------------------

Raw and Kube methods accept ``networks``, a list of existing Podman network names
or IDs. Omit it or use ``[]`` to retain Podman's existing defaults. FetchIt does
not create or delete these networks; create them on the same rootful or rootless
Podman instance that FetchIt uses before deploying:

.. code-block:: shell

   podman network create frontend
   podman network create backend

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads
     branch: main
     raw:
     - name: web
       targetPath: raw
       networks: [frontend, backend]
       schedule: '*/1 * * * *'
     kube:
     - name: pods
       targetPath: kube
       networks: [backend]
       schedule: '*/1 * * * *'

The Raw list applies to all containers from that method. A Raw JSON/YAML file can
override it with ``Networks: [frontend]``; an explicit ``Networks: []`` opts that
container back into defaults. Kube passes the method's list to Podman kube play
for all pods in its manifest. These are Podman networks, not Kubernetes Services
or NetworkPolicies. Network creation, IPs, aliases, and routing remain managed by
Podman. A named network list replaces implicit default-network attachment;
include the default network explicitly if needed. Configured networks are inspected before workload teardown. Missing networks
fail before removing existing containers or pods. A network disappearing after
this check, or another Podman runtime failure, can still interrupt redeployment. Network options are not applied
to file-transfer or other helper containers.

Changing a network setting in FetchIt's config alone does not redeploy an
unchanged Git manifest. Commit a change to the workload file to apply the new
attachments; existing workloads keep their current attachments until recreated.

HTTP image and disconnected ZIP archive downloads
-------------------------------------------------

Image URLs and disconnected ZIP archive URLs fetched over HTTP must return HTTP
200 after redirects. Other status codes fail the attempt before the response is
written to a local image or archive file. Image error responses are never imported
into Podman. The error includes the HTTP status code; check the source URL, access
permissions, and server availability before the next scheduled attempt. Image
failures are logged at error level. Failed archive refreshes stop reconciliation
for that attempt rather than applying cached repository content.

Incomplete transfers and invalid ZIP archives return errors. Temporary download
files are removed on failure, allowing retries. Existing local files are retained
on HTTP status failures. ZIP paths escaping the extraction directory and ZIP
symlinks are rejected. Legacy entries such as ``../fetchit/file`` remain supported
when their normalized paths resolve inside the destination ``fetchit`` directory.
Archive extraction is not transactional: a filesystem or
entry-read error during extraction can leave some extracted files behind.

Encrypted Kube manifests
------------------------

Kube methods can opt into authenticated SOPS decryption using a read-only age key
file. All changed manifests are prepared before teardown. See :doc:`sops` for a
complete encrypted Secret/Pod example, configuration, key rotation, deletion,
limits, and recovery. Ordinary Kube methods retain their current behavior.

Cleanup, rollback, and method identity
--------------------------------------

Raw, Kube, Quadlet, FileTransfer, and Systemd support opt-in method-removal
cleanup. Enable tracking while a method is still configured, before removing it.
FileTransfer and Systemd refuse to adopt pre-existing untracked files; follow the
migration steps in :doc:`lifecycle` before enabling cleanup on existing deployments.
Preserve the FetchIt volume and host journals across upgrades.

Git targets containing only Raw, Kube, or Quadlet may also opt into apply rollback:

.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/example/workloads.git
     branch: main
     rollback: true
     trackBadCommits: true
     raw:
     - name: web
       targetPath: containers/
       schedule: "*/1 * * * *"
       cleanupOnRemoval: true

``rollback`` defaults to false. ``trackBadCommits`` requires rollback and suppresses
repeated attempts at a failed revision only after successful rollback. Tracking is
process-local and resets on reload. Rollback is best effort and does not undo
persistent-data writes. It is unsupported for FileTransfer, Systemd, or Ansible.
See :doc:`lifecycle` for failure handling and retained resources.

Raw and Kube workloads carry FetchIt ownership labels. Keep method names and
paths stable, and avoid resource-name collisions between methods. Changing from
``containers`` to ``containers/`` retains the same normalized identity. See the
Kube workload label section above for discovery commands and reserved label keys.

Git mirrors and service status
-------------------------------

Targets can configure ordered ``fallbackURLs`` while retaining ``url`` as their
primary identity. See :doc:`mirrors` for trusted mirror configuration, Git history
checks, and authentication. SSH setup is documented above; verify host keys
rather than disabling verification.

Set ``FETCHIT_STATUS_ADDR`` to expose optional ``/healthz`` and ``/status`` HTTP
endpoints. See :doc:`status` for binding and monitoring examples. Neither endpoint
is enabled by default.
