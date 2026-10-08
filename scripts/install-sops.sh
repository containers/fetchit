#!/usr/bin/env bash
set -euo pipefail
# Keep the version, checksums, and docs/sops.rst in sync. Upstream release assets
# are checked against committed digests rather than downloaded checksums.
arch=${1:?Specify amd64 or arm64}
destination=${2:?Specify destination path}
version=3.13.3
case "$arch" in
  amd64) checksum=e5bec3346a873ae91d871550f3e698c1aad962aff462a080e40f25fde17fef6b ;;
  arm64) checksum=53b0abacd38ef1b12a66d6c100956691b9cefce018d91f81e73ddf7438b94d77 ;;
  *) echo 'Unsupported SOPS architecture' >&2; exit 1 ;;
esac
staging=$(mktemp -d)
trap 'rm -rf "$staging"' EXIT
curl --fail --location --retry 3 --connect-timeout 15 --max-time 180 \
  "https://github.com/getsops/sops/releases/download/v${version}/sops-v${version}.linux.${arch}" \
  --output "$staging/sops"
printf '%s  %s\n' "$checksum" "$staging/sops" | sha256sum --check --status
install -m 0755 "$staging/sops" "$destination"
