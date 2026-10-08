.. this is a comment, it is not rendered
   when adding new *.rst files, reference them here
   in this index.rst for them to be rendered and added to the
   table of contents


FetchIt
=======
FetchIt reconciles Git-defined containers, host services, and files using Podman.
Start with :doc:`quick_start`, choose a deployment method in :doc:`methods`, and
use :doc:`quadlet` for host-managed Podman services. See :doc:`release_notes` for
recent features and :doc:`lifecycle` for opt-in cleanup and rollback.

The guides describe current main-branch functionality. Use engine and helper
images containing those changes; an older published image may lack new options.

.. toctree::
   quick_start
   samples
   purpose
   methods
   lifecycle
   sops
   mirrors
   quadlet
   status
   release_notes
   running
   documentation
