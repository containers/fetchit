

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

Systemd
-------
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
   
