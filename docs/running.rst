

Running
============
For running the engine the podman socket must be enabled. This can be enabled for the user account that will be running fetchit or for root.

User
----
For regular user accounts run the following to enable the socket.

.. code-block:: bash

   systemctl --user enable --now podman.socket

Within */run* a process will be started for the user to interact with the podman socket. Using your UID you can idenitfy the socket.

.. code-block:: bash
   
   export DOCKER_HOST=unix:///run/user/$(id -u)/podman/podman.sock

Root
----
For the root user enable the socket by running the following.

.. code-block:: bash

   systemctl enable --now podman.socket

Launching
---------
The podman engine can be launched by running the following command or by using the systemd files from the repository. Most methods except for systemd can be ran without sudo. 

Running with systemd
--------------------
The two systemd files are differentiated by .root and .user.

Ensure that the location of the `config.yaml` is correctly defined in the systemd service file before attempting to start the service.

For root

.. code-block:: bash
   
   cp systemd/fetchit-root.service /etc/systemd/system/fetchit.service
   systemctl enable fetchit --now


For user ensure that the path for the configuration file `/home/fetchiter/config.yaml:/opt/config.yaml` and the path for the podman socket are correct.

.. code-block:: bash
   
   mkdir -p ~/.config/systemd/user/
   cp systemd/fetchit-user.service ~/.config/systemd/user/
   systemctl --user enable fetchit --now

Manually
--------

.. code-block:: bash
   
   podman run -d --name fetchit \
     -v fetchit-volume:/opt \
     -v ./config.yaml:/opt/config.yaml \
     -v /run/user/1000/podman/podman.sock:/run/podman/podman.sock \
     --security-opt label=disable \
     quay.io/fetchit/fetchit:latest

FetchIt will clone the repository and attempt to remediate those items defined in the config.yaml file. To follow the status.

.. code-block:: bash

   podman logs -f fetchit
   


Safe remote configuration updates
---------------------------------

``FETCHIT_CONFIG_URL`` and ``configReload.configURL`` accept a remote YAML config.
FetchIt requires HTTP 200, a single nonempty YAML mapping, and fields that decode
into the FetchIt configuration schema. Unknown fields, malformed YAML, empty
responses, and HTTP error pages are rejected. Requests time out after 30 seconds
and downloaded configs are limited to 1 MiB. Explicit empty lists such as
``targetConfigs: []`` are valid; an empty document is not.

A failed download or validation leaves ``config.yaml`` and its existing backup
unchanged. Config and backup are staged before replacement. Failed config replacement leaves
the old backup untouched; failed backup publication rolls the config back. A
rollback failure is reported explicitly and the installed config is loaded. The
two-file update is recoverable during ordinary errors, not a crash-atomic
transaction. Successful changed downloads save the previous bytes to
``config-backup.yaml`` and replace ``config.yaml`` using a staged file and atomic
rename, with mode 0600. Identical downloads do not trigger a reload. Validation
checks syntax and field decoding; it does not test network connectivity, image
availability, or every method's runtime requirements.

Mount a writable configuration **directory** at ``/opt/mount`` when using remote
reloads. A read-only mount or individual ``config.yaml`` file bind mount cannot
support atomic replacement: the update fails and preserves the current file.
On first startup without a local config, an invalid remote response does not
create a config file; correct the source and restart FetchIt. On scheduled
reloads, correct the source and FetchIt retries at the next configured interval.
