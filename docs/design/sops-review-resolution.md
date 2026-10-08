# SOPS review resolution

PR #401 implements issue #349 with Podman Go bindings v5.8.8 and SOPS 3.13.3.

Addressed findings:
- Production uses a constant executable; alternate executable paths are confined
  to tests. Ordinary age recipients require the exact public-key format, and the
  child PATH has no tools. Cloud backends, key groups, and plugins are rejected.
- data/stringData values must be encrypted. Integrity covers visible resource
  identities; partial MAC coverage is rejected.
- Old/new input comes from immutable Git blobs. Tests cover a changed working
  tree and historical deletion, plus deterministic failure of a mixed batch.
- Network preflight only runs when new manifests will be played; deletion-only
  and empty changes do not depend on the configured network.
- Key-path containment fails closed. Decryption limits/cancellation have typed
  causes and unsafe error text is hidden by preparation/Podman error wrappers.
- Diagnostic volume is bounded and discarded. Plaintext buffers are cleared on
  preparation failure and after apply. Cleanup/execution is shared across entry
  points. Podman per-container start errors are treated as failures.
- The user guide includes explicit private Podman secret mount permissions,
  version availability, dedicated resource names, and recovery instructions.
- The new workflow pins external Actions and the Alpine test image. Fedora uses
  a disposable GitHub-hosted runner with a privileged nested Podman container;
  the host cgroup namespace and explicit host cgroup mount are not exposed.
  Fedora uses vfs storage and a pinned pause image to avoid nested overlay mount
  constraints. The rootless Ubuntu fixture uses host networking to avoid the
  runner AppArmor policy denying rootless network-helper termination; production
  networking is not changed. This verifies Fedora userspace, not native Fedora
  SELinux policy enforcement.

Findings retained as deliberate choices:
- Podman 5's supported kind list excludes Service and StatefulSet. The supported
  list used here matches the targeted bindings/server sources and Podman 5 docs;
  it does not pretend to support every Kubernetes API kind.
- Podman kube play uses unqualified resource names. Two Pods with the same name
  in different Kubernetes namespaces still collide in the same Podman instance;
  namespace must not be used to bypass duplicate-name validation.
- Ubuntu 26.04 is an available runner in this repository. Actual jobs on that
  runner completed, including the full rootful lifecycle. Downgrading to 24.04
  would undo the project's intentional distro Podman 5 setup.
- User/release documentation describes the implemented source revision and the
  Unreleased feature. Older images are explicitly excluded.

Sources:
- https://docs.podman.io/en/v5.6.2/markdown/podman-kube-play.1.html
- Podman v5.8.8 pkg/domain/infra/abi/play.go and
  pkg/specgen/generate/kube/kube.go in the module used by FetchIt.
- https://getsops.io/docs/usage/advanced/
