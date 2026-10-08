#!/usr/bin/env bash
set -euo pipefail

fail() {
    echo "::error::$*" >&2
    exit 1
}

for tool in podman crun dpkg; do
    command -v "$tool" >/dev/null || fail "$tool is missing; run the Podman package installation step on Ubuntu 26.04."
done

podman_output=$(podman --version) || fail "Could not read the Podman version; check the installed podman package."
crun_output=$(crun --version) || fail "Could not read the crun version; check the installed crun package."
printf '%s\n%s\n' "$podman_output" "$crun_output"
podman_version=$(awk 'NR == 1 {print $3}' <<< "$podman_output")
crun_version=$(awk 'NR == 1 {print $3}' <<< "$crun_output")

[[ "$podman_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]] || fail "Unrecognized Podman version: $podman_output"
[[ "$crun_version" =~ ^[0-9]+\.[0-9]+ ]] || fail "Unrecognized crun version: $crun_output"

if ! dpkg --compare-versions "$podman_version" ge 5.7.0 ||
   ! dpkg --compare-versions "$podman_version" lt 6.0.0; then
    fail "Podman $podman_version is unsupported: Fetchit CI requires Podman >= 5.7.0 and < 6.0.0. Check Ubuntu 26.04's podman package; retain Podman 5 until the libraries are migrated."
fi

# crun 1.18 was the project's known-compatible runtime before using distro packages.
if ! dpkg --compare-versions "$crun_version" ge 1.18; then
    fail "crun $crun_version is unsupported: Fetchit CI requires crun >= 1.18 for Podman 5. Install a compatible crun package."
fi
