

Dependencies
============

Required
--------
For compiling the engine the following packages are required.

- gpgme-devel
- libbtrfs
- btrfs-progs-devel
- device-mapper-devel


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
