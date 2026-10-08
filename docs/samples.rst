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
     - ``quay.io/fetchit/fetchit-sample-app:latest``
     - FetchIt welcome page at ``http://localhost:9191``
   * - Raw, ``examples/raw``
     - ``quay.io/fetchit/fetchit-sample-app:latest``
     - HTTP on host ports 8080, 9080, 7070, and 9090
   * - Kube, ``examples/kube/3-example.yaml``
     - ``quay.io/fetchit/fetchit-sample-app:latest``
     - HTTP on host port 7080; ConfigMap supplies environment variables
   * - Kube PVC, ``examples/kube/2-example.yaml``
     - ``quay.io/fetchit/fetchit-sample-app:latest``
     - Host port 8080 serves content from ``task-pv-claim``
   * - Legacy Systemd, ``examples/systemd/httpd.service``
     - ``registry.access.redhat.com/ubi8/httpd-24:latest``
     - HTTP on host port 8080, container port 8080
   * - Quadlet, ``examples/quadlet``
     - ``docker.io/library/alpine:3.22``
     - Writes the configured message to the named volume; no HTTP port
   * - Image archive, ``examples/imageLoad``
     - Locally imported tag ``quay.io/notreal/httpd:latest``
     - Host port 9090, container port 8080; import an Apache image archive first
   * - Rollback, ``examples/rollback``
     - ``quay.io/fetchit/fetchit-sample-app:latest`` for working definitions
     - Ports 8080/8081; ``2-broken.yaml`` deliberately references an invalid registry

Run examples separately because several publish the same host ports. Stop the
previous example before starting another. The HTTP sample runs as user 1001
and listens on container port 8080. The ``cap.json`` and ``cap.yaml`` fixtures
use the image's non-root defaults without ``CapAdd`` or ``CapDrop`` overrides.

The former color-demo images have been replaced with a FetchIt welcome page.
Names such as ``colors1`` and environment values such as ``APP_COLOR`` are
retained for configuration/reconciliation demonstrations. The sample does not
use ``APP_COLOR`` to change its page.

The PVC example mounts a volume over the bundled website at ``/var/www/html``.
Supply readable content in that volume to get a successful homepage response.
For example, initialize the volume as root while the HTTP server remains user
1001:

.. code-block:: bash

   podman exec --user 0 nginx-pod-nginx-server sh -c \
     'printf "Hello from the sample volume\n" > /var/www/html/index.html'
   curl --fail http://localhost:8080/

For archive loading, save the Apache variant for the destination architecture
with the local tag used by the example:

.. code-block:: bash

   podman pull quay.io/fetchit/fetchit-sample-app:latest
   podman tag quay.io/fetchit/fetchit-sample-app:latest quay.io/notreal/httpd:latest
   podman save -o httpd.tar quay.io/notreal/httpd:latest

Serve the resulting file at the URL configured in
``examples/imageLoad-config.yaml`` or copy it onto the configured device. The
``quay.io/notreal`` name is only a local archive tag; it is not a published image.
Create the archive on a matching host or explicitly pull the destination
platform before saving. An amd64 archive cannot become arm64 by retagging it.

Updates and testing
-------------------

The HTTP sample source lives in ``examples/sample-app``. Its Containerfile uses
Red Hat's UBI 8 HTTP Server image, pinned to a multi-platform index digest, and
adds a small welcome page. Updating the base digest must pass both native
architecture tests. After successful tests on a main-branch push, Actions
publishes the tested amd64 and arm64 images as one multi-platform index at
``quay.io/fetchit/fetchit-sample-app:latest`` using the existing Quay publishing
credentials. Pull requests, scheduled checks, and manual test runs never publish.

Registries require network access. Pin the published multi-platform index digest
or mirror all platform manifests for reproducible/offline deployment. Copying
just the host's variant would reintroduce the architecture limitation.

The ``Sample applications (amd64 and arm64)`` GitHub Actions workflow runs on
native Ubuntu amd64 and arm64 runners for each PR, main push, weekly schedule,
and manual dispatch. It uses FetchIt's actual Raw/Kube routines and the
checked-in manifests, checks the loaded image architecture, and checks successful
HTTP responses and the non-root HTTP sample user. It also tests local image-archive import and runs the Systemd
and Quadlet sample images. Full service/receipt lifecycle coverage remains in
the existing Systemd and Quadlet workflows.

To run the native sample tests on a disposable Linux host with Podman 5:

.. code-block:: bash

   sudo podman build -t quay.io/fetchit/fetchit-sample-app:latest \
     -f examples/sample-app/Containerfile examples/sample-app
   sudo systemctl enable --now podman.socket
   go test -mod=readonly \
     -tags 'containers_image_openpgp gssapi providerless netgo osusergo exclude_graphdriver_btrfs samples_integration' \
     -c -o /tmp/fetchit-samples.test ./pkg/engine
   cd pkg/engine
   sudo env SAMPLE_TEST_SOCKET=unix:///run/podman/podman.sock \
     /tmp/fetchit-samples.test -test.v -test.run '^TestSampleApplications$'

The test creates and removes the sample container/pod names and PVC volume.
Use a disposable host rather than a system already running these examples.
