# FetchIt

FetchIt brings GitOps to hosts running Podman. Define containers, pods, host
services, or files in Git, and FetchIt reconciles them on a configured schedule—
without requiring a Kubernetes cluster.

The project is under active development. The guides describe current `main`;
new features are **unreleased** until included in a published release. Use an
engine and compatible helper built from current main to try them. A cached
`latest` image may not include the newest changes.

## What you can do

- **Deploy Quadlet bundles:** Manage Podman/systemd services from Git, including
  rootless operation, supporting files, drop-ins, and update/removal handling.
  [Quadlet guide](docs/quadlet.rst)
- **Opt into lifecycle cleanup and recovery:** Track owned containers, pods,
  copied files, and service units when methods leave configuration. Raw, Kube,
  and Quadlet targets can also opt into best-effort apply rollback.
  [Lifecycle guide](docs/lifecycle.rst)
- **Use Podman kube play:** Deploy ordinary or SOPS-encrypted YAML, attach Pods
  to existing Podman networks, and discover workloads through ownership labels.
  [Methods and networks](docs/methods.rst) · [SOPS guide](docs/sops.rst)
- **Keep Git sources available:** Authenticate with verified SSH host keys and
  configure trusted fallback repositories.
  [SSH configuration](docs/methods.rst) · [Mirrors guide](docs/mirrors.rst)
- **Monitor and try real examples:** Enable optional health/status endpoints and
  run Red Hat UBI-based HTTP samples on amd64 or arm64.
  [Status guide](docs/status.rst) · [Sample applications](docs/samples.rst)

Cleanup and rollback default to **off**. They do not undo arbitrary Ansible
changes or persistent-data writes. Preserve the engine volume and host ownership
journals, and read the lifecycle guide before enabling cleanup on existing files
or services. Trailing `/` in Git `targetPath` is optional; FileTransfer retains
its basename-based destination layout.

[Full documentation](https://fetchit.readthedocs.io/) ·
[Release notes](docs/release_notes.rst) · [Running guide](docs/running.rst)

## Requirements

- A Linux Podman host. For the documented host-managed features, use **Podman
  5.7 or newer within major version 5**. Podman 6 is not supported by Quadlet.
- An accessible Podman API socket for the intended rootful or rootless instance.
- Host systemd and cgroup v2 for Quadlet; a running host systemd manager for
  Systemd methods. Rootless services require the matching host user manager.
- **Go 1.27.1** or automatic Go toolchain downloads when building from source.

CI tests Ubuntu 26.04 and Fedora 44 userspace with Podman 5. ARM image builds and
sample checks run on native arm64 runners. Rootful and rootless image, network,
container, and volume stores are separate; use one scope consistently.

## Quick start

On a Linux host with Podman installed, run these commands as the intended host
user. For Fedora, install Podman with `sudo dnf install -y podman` first.

```sh
systemctl --user enable --now podman.socket
mkdir -p "$HOME/.fetchit"
```

Save this as `$HOME/.fetchit/config.yaml`:

```yaml
targetConfigs:
- url: https://github.com/containers/fetchit
  branch: main
  raw:
  - name: welcome-to-fetchit
    targetPath: examples/single-raw
    schedule: "*/1 * * * *"
    pullImage: true
```

Start FetchIt:

```sh
podman run -d --name fetchit \
  -v fetchit-volume:/opt \
  -v "$HOME/.fetchit:/opt/mount" \
  -v "/run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock" \
  --security-opt label=disable \
  quay.io/fetchit/fetchit:latest

podman logs -f fetchit
```

FetchIt reads `/opt/mount/config.yaml`. After deployment, open
<http://localhost:9191/> or run:

```sh
curl --fail http://localhost:9191/
podman ps
```

The HTTP sample uses `quay.io/fetchit/fetchit-sample-app:latest`, built from Red
Hat UBI HTTP Server with native amd64/arm64 variants. It runs as user 1001 on
container port 8080 without capability overrides. See the
[sample guide](docs/samples.rst) for other ports, PVCs, and offline image archives.

For this static configuration, restart FetchIt after changing `config.yaml`.
Workload changes in Git are applied on schedule. To reload configuration without
a restart, configure `configReload` and retain the writable directory mount;
see [remote configuration updates](docs/running.rst).

For workloads that must survive logout, enable lingering for the host user:

```sh
sudo loginctl enable-linger "$(id -un)"
```

See [Running](docs/running.rst) for rootful launches, engine systemd services, and
host identity settings. See [Quadlet](docs/quadlet.rst) for rootless/rootful bundle
examples. FileTransfer destinations must already exist on the host.

### Stop the demonstration

```sh
podman stop fetchit
podman rm fetchit
podman rm -f welcome
```

Stopping the engine does not stop its workloads automatically. These commands
remove only this demonstration's engine and named sample container. Keep
`fetchit-volume` for Git baselines and pending cleanup receipts; delete it only
when intentionally abandoning that state after completing any required cleanup.

## Build and try current main

From a checkout, prepare vendored dependencies and build for the host's
architecture:

```sh
go mod vendor
podman build --build-arg ARCH=amd64 -t localhost/fetchit:development .
```

Use `ARCH=arm64` on arm64. Substitute `localhost/fetchit:development` for the
engine image in the launch command. Build and launch in the same Podman store;
use `sudo podman` consistently for rootful deployments.

Set Quadlet's `helperImage: localhost/fetchit:development` to use the matching
helper. Tracked FileTransfer/Systemd cleanup currently uses the fixed helper
reference `quay.io/fetchit/fetchit:latest`; tag the reviewed local build with that
name in the same store before testing those features:

```sh
podman tag localhost/fetchit:development quay.io/fetchit/fetchit:latest
```

For production Quadlet deployments, use an approved immutable helper digest.
See [image compatibility and build guidance](docs/documentation.rst) and
[helper integrity guidance](docs/quadlet.rst).

## Develop and validate

On Linux, install the GPGME, device-mapper, and libseccomp development packages
before running unit tests:

```sh
go test -mod=readonly -tags 'containers_image_openpgp gssapi providerless netgo osusergo exclude_graphdriver_btrfs' ./...
```

Real SOPS tests additionally require `SOPS_TEST_BINARY` and `AGE_TEST_BINARY`
pointing to the supported SOPS executable and `age-keygen`. CI installs these
tools and runs the unit, ownership, rollback, and runtime lifecycle checks.
Integration tests use disposable hosts and packaged Podman 5/crun; do not run
host service tests against a personal Podman socket.

Build all documentation with the pinned dependencies:

```sh
python3 -m venv /tmp/fetchit-docs-venv
/tmp/fetchit-docs-venv/bin/python -m pip install -r docs/requirements.txt
/tmp/fetchit-docs-venv/bin/python -m sphinx -W -E -b html docs /tmp/fetchit-docs
```

Read the Docs must build the updated Git revision to publish these guides.
Historical release tags retain their old configuration. See
[documentation publishing](docs/documentation.rst) if the hosted site is stale.
