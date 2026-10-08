Sample applications
===================

The checked-in runnable application examples use images with native Linux
``amd64`` and ``arm64`` variants. These are demonstration workloads for Podman 5;
they do not require a Kubernetes cluster. Podman selects the image variant that
matches its Linux host. On macOS, the applications run inside the Podman machine;
follow the host/socket setup appropriate to that environment.

Images and ports
----------------

.. list-table::
   :header-rows: 1
   :widths: 25 40 35

   * - Example
     - Image
     - Result
   * - Quick start, ``examples/single-raw``
     - ``docker.io/library/httpd:2.4-alpine``
     - Apache welcome page at ``http://localhost:9191``
   * - Raw, ``examples/raw``
     - ``docker.io/library/httpd:2.4-alpine``
     - HTTP on host ports 8080, 9080, 7070, and 9090
   * - Kube, ``examples/kube/3-example.yaml``
     - ``docker.io/library/httpd:2.4-alpine``
     - HTTP on host port 7080; ConfigMap supplies environment variables
   * - Kube PVC, ``examples/kube/2-example.yaml``
     - ``docker.io/library/nginx:stable-alpine``
     - Host port 8080 serves content from ``task-pv-claim``
   * - Legacy Systemd, ``examples/systemd/httpd.service``
     - ``registry.access.redhat.com/ubi8/httpd-24:latest``
     - HTTP on host port 8080, container port 8080
   * - Quadlet, ``examples/quadlet``
     - ``docker.io/library/alpine:3.22``
     - Writes the configured message to the named volume; no HTTP port
   * - Image archive, ``examples/imageLoad``
     - Locally imported tag ``quay.io/notreal/httpd:latest``
     - Host port 9090, container port 80; import an Apache image archive first
   * - Rollback, ``examples/rollback``
     - ``docker.io/library/httpd:2.4-alpine`` for working definitions
     - Ports 8080/8081; ``2-broken.yaml`` deliberately references an invalid registry

Run examples separately because several publish the same host ports. Stop the
previous example before starting another. The capability-drop Raw example keeps
Apache's required startup capabilities and drops ``NET_RAW``; it is not a
complete container-hardening example.

The former color-demo images have been replaced with Apache's default
``It works!`` page. Names such as ``colors1`` and environment values such as
``APP_COLOR`` are retained for configuration/reconciliation demonstrations.
Apache does not use ``APP_COLOR`` to change its page. Use your own application
image when you want a visual response to environment changes.

The PVC example mounts a volume over nginx's bundled website. Supply an
``index.html`` in that volume to get a successful homepage response, for example:

.. code-block:: bash

   podman exec nginx-pod-nginx-server sh -c \
     'printf "Hello from the sample volume\n" > /usr/share/nginx/html/index.html'
   curl --fail http://localhost:8080/

For archive loading, save the Apache variant for the destination architecture
with the local tag used by the example:

.. code-block:: bash

   podman pull docker.io/library/httpd:2.4-alpine
   podman tag docker.io/library/httpd:2.4-alpine quay.io/notreal/httpd:latest
   podman save -o httpd.tar quay.io/notreal/httpd:latest

Serve the resulting file at the URL configured in
``examples/imageLoad-config.yaml`` or copy it onto the configured device. The
``quay.io/notreal`` name is only a local archive tag; it is not a published image.
Create the archive on a matching host or explicitly pull the destination
platform before saving. An amd64 archive cannot become arm64 by retagging it.

Updates and testing
-------------------

These upstream-maintained image tags receive updates without a FetchIt-owned
sample-image publishing pipeline. Registries still require network access and
may enforce pull limits. Pin a multi-platform image-index digest or use a trusted
multi-platform registry mirror when you need reproducible/offline deployment.
Copy all platform manifests when mirroring; copying just the host's variant
would reintroduce the architecture limitation.

The ``Sample applications (amd64 and arm64)`` GitHub Actions workflow runs on
native Ubuntu amd64 and arm64 runners for each PR, main push, weekly schedule,
and manual dispatch. It uses FetchIt's actual Raw/Kube routines and the
checked-in manifests, checks the loaded image architecture, and checks successful
HTTP responses. It also tests local image-archive import and runs the Systemd
and Quadlet sample images. Full service/receipt lifecycle coverage remains in
the existing Systemd and Quadlet workflows.

To run the native sample tests on a disposable Linux host with Podman 5:

.. code-block:: bash

   sudo systemctl enable --now podman.socket
   go test -mod=readonly \
     -tags 'containers_image_openpgp gssapi providerless netgo osusergo exclude_graphdriver_btrfs samples_integration' \
     -c -o /tmp/fetchit-samples.test ./pkg/engine
   cd pkg/engine
   sudo env SAMPLE_TEST_SOCKET=unix:///run/podman/podman.sock \
     /tmp/fetchit-samples.test -test.v -test.run '^TestSampleApplications$'

The test creates and removes the sample container/pod names and PVC volume.
Use a disposable host rather than a system already running these examples.
