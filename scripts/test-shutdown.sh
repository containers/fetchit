#!/bin/bash
# Run against a disposable Linux Podman host with its API socket enabled.
set -euo pipefail

image=${1:-quay.io/fetchit/fetchit:latest}
socket=${PODMAN_SOCKET:-/run/podman/podman.sock}
name="fetchit-shutdown-$$"
config=$(mktemp -d)
cleanup() {
	podman rm -f "$name" >/dev/null 2>&1 || true
	rm -rf "$config"
}
trap cleanup EXIT
printf 'targetConfigs: []\n' > "$config/config.yaml"

for signal in TERM INT; do
	podman run -d --name "$name" \
		-v "$config:/opt/mount" \
		-v "$socket:/run/podman/podman.sock" \
		--security-opt label=disable \
		-e FETCHIT_STATUS_ADDR=:8080 \
		-e 'FETCHIT_CONFIG=targetConfigs: []' \
		-p 127.0.0.1::8080 "$image"
	port=$(podman port "$name" 8080/tcp)
	ready=false
	for attempt in {1..60}; do
		if curl --fail --silent --max-time 1 "http://$port/healthz" >/dev/null; then
			ready=true
			break
		fi
		sleep 0.5
	done
	if [[ "$ready" != true ]]; then
		podman logs "$name"
		echo 'engine did not become ready' >&2
		exit 1
	fi
	# A Bash wrapper at PID 1 was the cause of issue #287.
	process=$(podman top "$name" pid,args)
	if [[ "$process" == *entry.sh* ]]; then
		echo "entry script still wraps FetchIt: $process" >&2
		exit 1
	fi
	started=$SECONDS
	if [[ "$signal" == TERM ]]; then
		podman container stop "$name"
	else
		podman kill --signal INT "$name"
		timeout 8 podman wait "$name"
	fi
	elapsed=$((SECONDS - started))
	exit_code=$(podman inspect --format '{{.State.ExitCode}}' "$name")
	podman logs "$name"
	if (( elapsed >= 8 )) || [[ "$exit_code" != 0 ]]; then
		echo "$signal shutdown failed: ${elapsed}s, exit $exit_code" >&2
		exit 1
	fi
	echo "$signal shutdown passed: ${elapsed}s, exit $exit_code"
	podman rm "$name"
done
