#!/usr/bin/env bash
# Publish only the supplied, tested images; pin index entries to the pushed digests.
set -euo pipefail
[[ $# -eq 3 ]] || { echo 'Usage: publish-platform-index.sh DESTINATION:latest AMD64_IMAGE ARM64_IMAGE' >&2; exit 1; }
destination=$1
amd_image=$2
arm_image=$3
[[ "$destination" == *:latest ]] || { echo 'Destination must include :latest' >&2; exit 1; }
repository=${destination%:latest}
registry_tls_verify=${FETCHIT_REGISTRY_TLS_VERIFY:-true}
if [[ "$registry_tls_verify" != true && "$registry_tls_verify" != false ]]; then
  echo 'Invalid TLS verification setting' >&2
  exit 1
fi
registry=${repository%%/*}
if [[ "$registry_tls_verify" == false && "$registry" != 127.0.0.1:* && "$registry" != localhost:* ]]; then
  echo 'TLS verification may only be disabled for loopback registries' >&2
  exit 1
fi
for reference in "$destination" "$amd_image" "$arm_image"; do
  if [[ -z "$reference" || "$reference" == -* || "$reference" == *[[:space:]]* ]]; then
    echo 'Invalid image reference' >&2
    exit 1
  fi
done
options=("--tls-verify=$registry_tls_verify")
if [[ -n ${FETCHIT_PUBLISH_AUTH_FILE:-} ]]; then options+=(--authfile="$FETCHIT_PUBLISH_AUTH_FILE"); fi
scratch=$(mktemp -d)
manifest="localhost/fetchit-publish-$$"
trap 'publish_status=$?; podman manifest rm "$manifest" >/dev/null 2>&1 || true; rm -rf "$scratch"; exit "$publish_status"' EXIT

test "$(podman image inspect --format '{{.Architecture}}' "$amd_image")" = amd64
test "$(podman image inspect --format '{{.Architecture}}' "$arm_image")" = arm64
# A push may convert the archive's manifest and update its storage metadata. Do
# not reuse an index assembled from those local instances before the pushes.
# Retain legacy architecture repositories for existing consumers.
podman push "${options[@]}" --format=v2s2 "$amd_image" "docker://$repository-amd:latest"
podman push "${options[@]}" --format=v2s2 "$arm_image" "docker://$repository-arm:latest"
# Registries require every index child manifest in the index's own repository.
podman push "${options[@]}" --format=v2s2 --digestfile "$scratch/amd.digest" "$amd_image" "docker://$repository:amd64"
podman push "${options[@]}" --format=v2s2 --digestfile "$scratch/arm.digest" "$arm_image" "docker://$repository:arm64"
amd_digest=$(cat "$scratch/amd.digest")
arm_digest=$(cat "$scratch/arm.digest")
for digest in "$amd_digest" "$arm_digest"; do
  [[ "$digest" =~ ^sha256:[0-9a-f]{64}$ ]] || { echo 'Invalid pushed image digest' >&2; exit 1; }
done
podman manifest create "$manifest"
podman manifest add "${options[@]}" "$manifest" "docker://$repository@$amd_digest"
podman manifest add "${options[@]}" "$manifest" "docker://$repository@$arm_digest"
# Child manifests and blobs already exist in this repository. Copying them again
# can recompress layers and replace the exact digests pinned above. Publish only
# the index; keep the registry verification below strict.
podman manifest push --all=false "${options[@]}" --format=docker "$manifest" "docker://$destination"
podman manifest inspect "${options[@]}" "$destination" > "$scratch/index.json"
python3 - "$scratch/index.json" "$amd_digest" "$arm_digest" <<'PYTHON'
import json
import sys
with open(sys.argv[1]) as source:
    manifests = json.load(source)['manifests']
expected = {('linux', 'amd64'): sys.argv[2], ('linux', 'arm64'): sys.argv[3]}
actual = {(m['platform']['os'], m['platform']['architecture']): m['digest'] for m in manifests}
if len(manifests) != 2 or actual != expected:
    raise ValueError(f"Unexpected platform index: actual={actual!r}, expected={expected!r}")
PYTHON
