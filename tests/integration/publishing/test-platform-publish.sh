#!/usr/bin/env bash
set -euo pipefail
export XDG_RUNTIME_DIR="/run/user/$(id -u)"
registry_digest=sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373
registry_image="mirror.gcr.io/library/registry@$registry_digest"
if ! podman pull "$registry_image"; then
  registry_image="docker.io/library/registry@$registry_digest"
  podman pull "$registry_image"
fi
podman run -d --rm --name publish-registry --network host \
  -e REGISTRY_HTTP_ADDR=127.0.0.1:5000 \
  "$registry_image"
trap 'podman logs publish-registry; podman rm -f publish-registry' EXIT
timeout 30 bash -c 'until curl --fail --silent http://127.0.0.1:5000/v2/; do sleep 1; done'
for arch in amd64 arm64; do
  podman build --format oci --platform "linux/$arch" -t "localhost/publish-source:$arch" \
    -f tests/integration/publishing/Containerfile tests/integration/publishing
done
amd=$(podman image inspect --format '{{.Id}}' localhost/publish-source:amd64)
arm=$(podman image inspect --format '{{.Id}}' localhost/publish-source:arm64)
podman save --multi-image-archive -o /tmp/platforms.tar localhost/publish-source:amd64 localhost/publish-source:arm64
podman image rm -f "$amd" "$arm"
podman load -i /tmp/platforms.tar
expect_failure() {
  if "$@"; then
    echo 'Invalid publishing input unexpectedly succeeded' >&2
    exit 1
  fi
}
expect_failure bash scripts/publish-platform-index.sh
expect_failure bash scripts/publish-platform-index.sh 127.0.0.1:5000/test localhost/publish-source:amd64 localhost/publish-source:arm64
expect_failure env FETCHIT_REGISTRY_TLS_VERIFY=invalid bash scripts/publish-platform-index.sh 127.0.0.1:5000/test:latest localhost/publish-source:amd64 localhost/publish-source:arm64
expect_failure env FETCHIT_REGISTRY_TLS_VERIFY=false bash scripts/publish-platform-index.sh quay.io/fetchit/test:latest localhost/publish-source:amd64 localhost/publish-source:arm64
expect_failure bash scripts/publish-platform-index.sh 127.0.0.1:5000/test:latest localhost/publish-source:arm64 localhost/publish-source:amd64
expect_failure bash scripts/publish-platform-index.sh 127.0.0.1:5000/test:latest localhost/publish-missing:amd64 localhost/publish-source:arm64
# Seed an incorrect old architecture tag: the script must replace it.
podman push --tls-verify=false localhost/publish-source:arm64 docker://127.0.0.1:5000/fetchit-test-amd:latest
FETCHIT_REGISTRY_TLS_VERIFY=false bash scripts/publish-platform-index.sh \
  127.0.0.1:5000/fetchit-test:latest localhost/publish-source:amd64 localhost/publish-source:arm64
# Check both architecture tags before deliberately moving one.
for arch_tag in amd arm; do
  podman manifest inspect --tls-verify=false "127.0.0.1:5000/fetchit-test-$arch_tag:latest" > "/tmp/$arch_tag-tag.json"
done
podman manifest inspect --tls-verify=false 127.0.0.1:5000/fetchit-test:latest > /tmp/original-index.json
for arch_tag in amd arm; do
  architecture=amd64
  if [[ "$arch_tag" == arm ]]; then architecture=arm64; fi
  loaded=$(podman pull --quiet --tls-verify=false --policy=always "127.0.0.1:5000/fetchit-test-$arch_tag:latest")
  test "$(podman image inspect --format '{{.Architecture}}' "$loaded")" = "$architecture"
done
# Moving an architecture tag afterward must not change index membership.
podman push --tls-verify=false localhost/publish-source:arm64 docker://127.0.0.1:5000/fetchit-test-amd:latest
for arch in amd64 arm64; do
  loaded=$(podman pull --quiet --tls-verify=false --policy=always --platform "linux/$arch" 127.0.0.1:5000/fetchit-test:latest)
  test "$(podman image inspect --format '{{.Architecture}}' "$loaded")" = "$arch"
done
podman manifest inspect --tls-verify=false 127.0.0.1:5000/fetchit-test:latest > /tmp/retagged-index.json
cmp /tmp/original-index.json /tmp/retagged-index.json

# Recompression must not rewrite the already-published child manifests. Force
# different gzip levels for the architecture pushes and any attempted index-side
# child copies. This reproduces the main-branch failure with tiny test images.
wrapper_dir=$(mktemp -d)
real_podman=$(command -v podman)
export FETCHIT_TEST_REAL_PODMAN="$real_podman"
cat > "$wrapper_dir/podman" <<'WRAPPER'
#!/usr/bin/env bash
set -euo pipefail
if [[ ${1:-} == push ]]; then
  shift
  exec "$FETCHIT_TEST_REAL_PODMAN" push --compression-format=gzip --compression-level=1 --force-compression "$@"
elif [[ ${1:-} == manifest && ${2:-} == push ]]; then
  shift 2
  exec "$FETCHIT_TEST_REAL_PODMAN" manifest push --compression-format=gzip --compression-level=9 --force-compression "$@"
fi
exec "$FETCHIT_TEST_REAL_PODMAN" "$@"
WRAPPER
chmod +x "$wrapper_dir/podman"
PATH="$wrapper_dir:$PATH" FETCHIT_REGISTRY_TLS_VERIFY=false bash scripts/publish-platform-index.sh \
  127.0.0.1:5000/fetchit-recompression:latest localhost/publish-source:amd64 localhost/publish-source:arm64
rm -rf "$wrapper_dir"
for arch in amd64 arm64; do
  loaded=$(podman pull --quiet --tls-verify=false --policy=always --platform "linux/$arch" 127.0.0.1:5000/fetchit-recompression:latest)
  test "$(podman image inspect --format '{{.Architecture}}' "$loaded")" = "$arch"
done
