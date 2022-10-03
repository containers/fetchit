Quick Start
============
If you want to to try FetchIt out run the following commands. This document will assume that the OS is Fedora, CentOS, or RHEL but FetchIt is also tested on Ubuntu. The only requirement is Podman v4.

We will assume that FetchIt will be ran as a non-privileged user. The first step will be to install Podman.

.. code-block:: bash
   
   sudo dnf -y podman
   systemctl start podman.socket --user

Now that Podman is available and the Podman socket is running, we can use FetchIt to manage containers. Start by creating the directory that will hold the FetchIt configuration.

.. code-block:: bash
   
   mkdir ~/.fetchit


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

The sample application can be found by visting the following URL `on your localhost <http://localhost:9191>`_


With this demonstration in mind you can fork the FetchIt repository or create your own repository and start defining your own applications for FetchIt.

Any changes that you make to the configuration file require a restart of the FetchIt container.

