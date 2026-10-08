# Fetchit
The purpose of FetchIt is to allow for GitOps management of podman managed containers.

This project is currently under development. For a more detailed explanation of the project visit the docs page.
https://fetchit.readthedocs.io/

A quickstart example is available at https://github.com/containers/fetchit/blob/main/docs/quick_start.rst

## Requirements

- **Podman v5.7+ and below v6** (Go libraries use v5.8.8; Fedora 44 packages provide v5.8.7)
- **Go 1.27.1+** (for building from source)
- **Linux Kernel 5.2+** (required by Podman)

## Developing
To develop and test changes of FetchIt, the FetchIt image can be built locally and then run on the development system.

Run the unit tests on Linux with Go 1.27.1 and the GPGME, device-mapper,
and libseccomp development packages installed:

```sh
go test -mod=readonly -tags 'containers_image_openpgp gssapi providerless netgo osusergo exclude_graphdriver_btrfs' ./...
```

CI runs these tests on Ubuntu 26.04 and Fedora 44. Integration tests use
Ubuntu 26.04's packaged Podman 5 and crun, installed by a shared CI action
without building Podman from source. The action requires Podman 5.7+ and below
6, and crun 1.18+ (the runtime baseline previously used by this project).
Ubuntu 26.04 is a [generally available GitHub runner](https://github.com/actions/runner-images/issues/14747).

```
go mod tidy
go mod vendor
podman build . --file Dockerfile --tag quay.io/fetchit/fetchit-amd:latest
podman tag quay.io/fetchit/fetchit-amd:latest quay.io/fetchit/fetchit:latest
```

Once the image has been successfully built the image can be ran using the following command.

```

podman run -d --rm --name fetchit --security-opt label=disable -v fetchit-volume:/opt -v ./examples/readme-config.yaml:/opt/config.yaml -v /run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock quay.io/fetchit/fetchit:latest
```

##  Running
FetchIt requires the podman socket to be running on the host. The socket can be enabled for a specific user or for root.

To enable the socket for $USER:

```
systemctl --user enable podman.socket --now
```

To enable the socket for root:

```
systemctl enable podman.socket --now
```


#### Verify running containers before deploying fetchit.

```
podman ps

CONTAINER ID  IMAGE       COMMAND     CREATED     STATUS      PORTS       NAMES
```


### FetchIt launch options
FetchIt and can be started manually or launched via systemd.

Define the parameters in your `$HOME/.fetchit/config.yaml` to relate to your git repository.
This example can be found in [./examples/readme-config.yaml](examples/readme-config.yaml)

```
targetConfigs:
- url: https://github.com/containers/fetchit
  branch: main
  filetransfer:
  - name: ft-ex
    targetPath: examples/filetransfer
    destinationDirectory: /tmp
    schedule: "*/1 * * * *"
  raw:
  - name: raw-ex
    targetPath: examples/raw
    schedule: "*/1 * * * *"
```

#### Launch using systemd
Two systemd files are provided to allow for FetchIt to run as a user or as root. The files are under the systemd folder, differentiated by fetchit-root and fetchit-user.

Ensure that there is a config at `$HOME/.fetchit/config.yaml` before attempting to start the service.

For root
```
cp systemd/fetchit-root.service /etc/systemd/system/fetchit.service
systemctl enable fetchit --now
```

For $USER
```
mkdir -p ~/.config/systemd/user/
cp systemd/fetchit-user.service ~/.config/systemd/user/fetchit.service
systemctl --user enable fetchit --now
```

#### Manually launch the fetchit container using a podman volume

```
podman run -d --rm --name fetchit \
    -v fetchit-volume:/opt \
    -v $HOME/.fetchit:/opt/mount \
    -v /run/user/$(id -u)/podman/podman.sock:/run/podman/podman.sock \
    --security-opt label=disable \
    quay.io/fetchit/fetchit:latest
```

**NOTE:**
* If a podman volume is not the preferred storage solution a directory can be used as well.
An example would be `-v ~/fetchit-volume:/opt` instead of `-v fetchit-volume:/opt`.
* For filetransfer, the `destination directory must exist` on the host.

The container will be started and will run in the background. To view the logs:

```
podman logs -f fetchit


```

#### Verify the sample applications are running

```
podman ps

```

The Raw HTTP samples use `quay.io/fetchit/fetchit-sample-app:latest` on both amd64 and arm64.
The image is built from Red Hat UBI HTTP Server, runs as user 1001, and needs no capability overrides.
View the FetchIt welcome page at `http://localhost:8080` and `http://localhost:9080`,
or run `curl --fail http://localhost:8080/`. Container port 8080 maps to the existing
host ports. `APP_COLOR` demonstrates environment propagation and does not change
the page color. See [sample applications](https://fetchit.readthedocs.io/en/latest/samples.html)
for all examples, image-archive instructions, and native architecture tests.

#### Verify the file is placed on the host

```
watch ls -al /tmp/hello.txt
```

#### Clean up

```
podman stop colors1 colors2 fetchit && podman rm colors1 colors2 && podman volume rm fetchit-volume
```

## Health and status endpoint

Set `FETCHIT_STATUS_ADDR=127.0.0.1:8080` to enable optional `/healthz` and
`/status` HTTP endpoints. For containers, add
`-e FETCHIT_STATUS_ADDR=:8080 -p 127.0.0.1:8080:8080` to the launch command.
The status endpoint reports scheduled invocation attempts, including Quadlet;
liveness does not imply successful reconciliation. See the
[full status guide](docs/status.rst) for response fields, reload behavior, and access configuration.

### Building documentation

Use the pinned documentation dependencies shared by Read the Docs and GitHub Actions:

```bash
python3 -m venv .venv-docs
. .venv-docs/bin/activate
python -m pip install -r docs/requirements.txt
python -m sphinx -W -E -b html docs /tmp/fetchit-docs
```

Read the Docs must build a commit containing the current `.readthedocs.yml`.
Historical versions keep their own build configuration. If a build still checks
out an old commit, inspect the hosted project's version settings and trigger a
build of `main` after merging; changing this repository does not rewrite old tags.
