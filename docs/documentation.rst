Documentation builds and publishing
===================================

The repository documentation describes current main-branch functionality.
Read the Docs builds the configuration file from the selected Git revision;
rebuilding an old release tag does not use main's updated configuration.

Build locally
-------------

From the repository root, use an isolated Python environment:

.. code-block:: bash

   python3 -m venv /tmp/fetchit-docs-venv
   /tmp/fetchit-docs-venv/bin/python -m pip install -r docs/requirements.txt
   /tmp/fetchit-docs-venv/bin/python -m sphinx -W -E -b html docs /tmp/fetchit-docs-html

GitHub Actions runs the same strict Sphinx build for PRs and main pushes. Its
Python dependencies match the pinned requirements used by Read the Docs. CI
checks the documentation source; it does not itself publish the hosted site.

Build a compatible engine and helper
-------------------------------------

The host-file cleanup features require commit ``4e7964a`` or a descendant, which
also includes the other features documented here. This is a source compatibility
baseline, not a published release tag. For a reproducible development build:

.. code-block:: bash

   git clone https://github.com/containers/fetchit.git
   cd fetchit
   git checkout 4e7964a
   go mod vendor
   # Use ordinary podman for rootless or sudo podman for rootful, consistently.
   podman build --build-arg ARCH=amd64 -t localhost/fetchit:reviewed .

Use ``ARCH=arm64`` on arm64. Launch that engine image and set Quadlet's
``helperImage: localhost/fetchit:reviewed``. FileTransfer/Systemd tracked cleanup
currently uses the fixed ``quay.io/fetchit/fetchit:latest`` helper reference:
tag the same reviewed local image with that name in the matching Podman store
before testing those features. They do not expose Quadlet's ``helperImage``
override. Follow :doc:`running` and :doc:`quadlet` for mounts and host setup.
For production publication, review the source, publish your approved build, and
pin its digest where the method supports it; do not invent a compatible release
number or use an older image merely because it has the ``latest`` tag.

Configure Read the Docs
-----------------------

Connect the ``containers/fetchit`` GitHub repository and verify its integration
receives push events. Set the default branch to ``main``. Activate the ``main``
version and trigger a build, then select a successfully built version as the
project's default documentation version.

The root configuration file is ``.readthedocs.yml``. If a custom configuration
path is required in project settings, use that exact filename. It selects Ubuntu
24.04, Python 3.12, ``docs/requirements.txt``, and ``docs/conf.py``.

If ``latest`` refers to a Git tag rather than the main branch, rebuilding it may
continue to use an old configuration. Check the version's Git identifier and the
build's checkout revision. Until the alias is corrected, publish and select the
explicit ``main`` version. The default branch and default documentation version
are separate settings.

The error ``Config validation error in build.os. Value build not found`` means
the loaded configuration lacks the required build settings. Compare the
configuration shown in the failing build with the selected revision's
``.readthedocs.yml``. A historical release such as ``v0.0.1`` retains its historical
configuration; use a version containing the fix or backport it to a maintained
release branch. Do not move historical release tags merely to repair docs.

After a successful build, check :doc:`methods`, :doc:`quadlet`, :doc:`lifecycle`,
and :doc:`release_notes` on the hosted version. A successful local/Actions build
alone does not establish that Read the Docs has published the new revision.

Scheduled Git update testing
----------------------------

The image workflow's ``make-change-to-repo`` job runs on pull requests and pushes
using the engine and sample images built from that revision. It starts a
job-local, read-only smart HTTP Git server, deploys two raw workloads, commits
and pushes an update into a temporary bare repository, and waits for FetchIt's
normal minute-based polling to reconcile the new commit. The changed workload
must be replaced, the unchanged workload must retain its container ID, and the
FetchIt process must remain running without a restart.

The test does not write to GitHub, use repository-write credentials, force-push,
or modify the shared ``ci`` branch. Every job has its own Git repository, port,
containers, and state volume. Logs are printed on exit and fixtures are cleaned
up. To reproduce on a Linux host with rootful Podman, enable the Podman socket,
load or build the engine and sample images, then run:

.. code-block:: bash

   sudo systemctl enable --now podman.socket
   sudo env FETCHIT_TEST_ENGINE_IMAGE=localhost/fetchit:reviewed \
     bash tests/integration/live-update/test-live-update.sh

The sample image must be tagged ``quay.io/fetchit/fetchit-sample-app:latest`` in
the same rootful store. For a local build, use:

.. code-block:: bash

   sudo podman build -t quay.io/fetchit/fetchit-sample-app:latest \
     -f examples/sample-app/Containerfile examples/sample-app
