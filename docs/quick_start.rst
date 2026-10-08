Quick Start
============
If you want to try FetchIt out run the following commands. This document will assume that the OS is Fedora, CentOS, or RHEL but FetchIt is also tested on Ubuntu. Use Podman 5.7 or newer within major version 5. For host-managed Quadlet services, see :doc:`quadlet`.

We will assume that FetchIt will be ran as a non-privileged user. The first step will be to install Podman.

.. code-block:: bash
   
   sudo dnf install -y podman
   systemctl --user enable --now podman.socket

Now that Podman is available and the Podman socket is running, we can use FetchIt to manage containers. Start by creating the directory that will hold the FetchIt configuration.

.. code-block:: bash
   
   mkdir -p "$HOME/.fetchit"


Next, create a configuration file.

.. code-block:: bash
   
   vi ~/.fetchit/config.yaml
   
.. code-block:: yaml

   targetConfigs:
   - url: https://github.com/containers/fetchit
     raw:
     - name: welcome-to-fetchit
       targetPath: examples/single-raw
       schedule: "*/1 * * * *"
       pullImage: true
     branch: main

Finally, run FetchIt.

.. code-block:: bash
   
   podman run -d --rm --name fetchit -v fetchit-volume:/opt -v $HOME/.fetchit:/opt/mount -v /run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock --security-opt label=disable quay.io/fetchit/fetchit:latest


To view the running containers, run the following command.

.. code-block:: bash
   
   podman ps

The sample application can be found by visiting the following URL `on your localhost <http://localhost:9191>`_


The sample uses the Red Hat UBI-based ``quay.io/fetchit/fetchit-sample-app:latest`` image, which
provides native Linux amd64 and arm64 variants. You should see the FetchIt welcome page. The server runs as user 1001 on
container port 8080 without capability overrides. The same configuration works on either architecture; Podman
selects the appropriate image automatically. See :doc:`samples` for the other
examples, ports, and architecture regression tests.

.. code-block:: bash

   curl --fail http://localhost:9191/

With this demonstration in mind you can fork the FetchIt repository or create your own repository and start defining your own applications for FetchIt.

For this static configuration, restart FetchIt after editing the configuration
file. To reload configuration without a restart, configure ``configReload`` as
described in :doc:`methods` and use a writable directory mount; see :doc:`running`.
Changes to workload files in Git are applied on the configured schedule.

Keep ``fetchit-volume`` when recreating the engine. Deleting the engine container
alone does not remove its managed workloads. Cleanup and rollback are opt-in;
see :doc:`lifecycle` before removing a method from configuration. For services
that must survive logout, follow the lingering setup in :doc:`running`.

