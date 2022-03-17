

Running
============
For running the engine the podman socket must be enabled. This can be enabled for the user account that will be running harpoon or for root.

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
The podman engine can be launched by running the following command. Most methods except for systemd can be ran without sudo. 

.. code-block:: bash
   
   sudo setenforce 0
   podman run -d --name harpoon -v harpoon-volume:/opt -v ./config.yaml:/opt/config.yaml -v /run/user/1000/podman/podman.sock:/run/podman/podman.sock quay.io/harpoon/harpoon:latest

Harpoon will clone the repository and attempt to remediate those items defined in the config.yaml file. To follow the status.

.. code-block:: bash

   podman logs -f harpoon
   
