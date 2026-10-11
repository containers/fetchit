#!/usr/bin/env bash
# Verify existing self-configuration with a root YAML served from fixture Git.
set -euo pipefail
engine_image=${FETCHIT_TEST_ENGINE_IMAGE:-quay.io/fetchit/fetchit-amd:latest}
sample_image=quay.io/fetchit/fetchit-sample-app:latest
scratch=$(mktemp -d)
fixture_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../live-update" && pwd)
engine="fetchit-root-config-$$"
first="$engine-first"
second="$engine-second"
third="$engine-third"
state="$engine-state"
server_pid=
cleanup() {
  status=$?
  podman logs "$engine" || true
  cat "$scratch/server.log" || true
  podman rm -f "$engine" "$first" "$second" "$third" >/dev/null 2>&1 || true
  podman volume rm "$state" >/dev/null 2>&1 || true
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT
podman image exists "$engine_image"
podman image exists "$sample_image"
mkdir -p "$scratch/server" "$scratch/seed/apps/first" "$scratch/seed/apps/second" "$scratch/seed/apps/third" "$scratch/mount"
git init --bare --initial-branch=main "$scratch/server/live.git"
git -C "$scratch/server/live.git" config http.receivepack false
git init --initial-branch=main "$scratch/seed"
git -C "$scratch/seed" config user.name 'FetchIt root config fixture'
git -C "$scratch/seed" config user.email 'ci@example.invalid'
git -C "$scratch/seed" remote add origin "$scratch/server/live.git"
python3 "$fixture_dir/git-server.py" "$scratch/server" "$scratch/port" > "$scratch/server.log" 2>&1 &
server_pid=$!
for ((attempt=0;attempt<50;attempt++)); do
  if [[ -s "$scratch/port" ]]; then break; fi
  kill -0 "$server_pid"
  sleep 0.1
done
port=$(cat "$scratch/port")
url="http://127.0.0.1:$port/config.yaml"
write_workload() {
  local name=$1 color=$2 app=$3
  cat > "$scratch/seed/apps/$app/app.json" <<JSON
{"Image":"$sample_image","Name":"$name","Env":{"APP_COLOR":"$color"}}
JSON
}
write_root() {
  cat > "$scratch/seed/config.yaml" <<CONFIG
configReload:
  configURL: $url
  schedule: "*/1 * * * *"
targetConfigs:
- name: first-source
  url: http://127.0.0.1:$port/live.git
  branch: main
  raw:
  - name: first
    targetPath: apps/first
    schedule: "*/1 * * * *"
- name: second-source
  url: http://127.0.0.1:$port/live.git
  branch: main
  raw:
  - name: second
    targetPath: apps/second
    schedule: "*/1 * * * *"
CONFIG
  if [[ "$1" == expanded ]]; then
    cat >> "$scratch/seed/config.yaml" <<CONFIG
- name: third-source
  url: http://127.0.0.1:$port/live.git
  branch: main
  raw:
  - name: third
    targetPath: apps/third
    schedule: "*/1 * * * *"
CONFIG
  fi
}
publish() {
  git -C "$scratch/seed" add .
  git -C "$scratch/seed" commit -m "$1"
  git -C "$scratch/seed" push origin main
}
write_workload "$first" blue first
write_workload "$second" green second
write_workload "$third" purple third
write_root initial
publish initial
# No config exists locally: bootstrap must download config.yaml from Git.
test ! -e "$scratch/mount/config.yaml"
podman run -d --name "$engine" --network host --security-opt label=disable \
  -e FETCHIT_CONFIG_URL="$url" \
  -v "$state:/opt" -v "$scratch/mount:/opt/mount" \
  -v /run/podman/podman.sock:/run/podman/podman.sock "$engine_image"
wait_color() {
  local name=$1 color=$2 deadline=$((SECONDS+180))
  while ((SECONDS<deadline)); do
    if [[ $(podman inspect --format '{{.State.Running}}' "$engine" 2>/dev/null) != true ]]; then return 1; fi
    if [[ $(podman exec "$name" printenv APP_COLOR 2>/dev/null) == "$color" ]]; then return 0; fi
    sleep 1
  done
  echo "Timed out waiting for $name=$color" >&2
  return 1
}
wait_color "$first" blue
wait_color "$second" green
test -s "$scratch/mount/config.yaml"
initial_hash=$(sha256sum "$scratch/mount/config.yaml" | awk '{print $1}')
engine_id=$(podman inspect --format '{{.Id}}' "$engine")
! podman container exists "$third"
# The root adds a method and existing Git methods receive updated workload data.
write_workload "$first" pink first
write_root expanded
publish expand
wait_color "$third" purple
wait_color "$first" pink
wait_color "$second" green
test "$(podman inspect --format '{{.Id}}' "$engine")" = "$engine_id"
test "$(podman inspect --format '{{.RestartCount}}' "$engine")" = 0
test "$(sha256sum "$scratch/mount/config-backup.yaml" | awk '{print $1}')" = "$initial_hash"
# Invalid root data cannot replace the active root or add/remove methods.
active_hash=$(sha256sum "$scratch/mount/config.yaml" | awk '{print $1}')
printf 'targetConfigz: []\n' > "$scratch/seed/config.yaml"
publish invalid-root
# Wait beyond one scheduled config check; the fixture log proves it was read.
for ((deadline=SECONDS+100;SECONDS<deadline;)); do
  if podman logs "$engine" 2>&1 | awk '/invalid downloaded config/ {found=1} END {exit !found}'; then break; fi
  sleep 1
done
podman logs "$engine" 2>&1 | awk '/invalid downloaded config/ {found=1} END {exit !found}'
test "$(sha256sum "$scratch/mount/config.yaml" | awk '{print $1}')" = "$active_hash"
wait_color "$first" pink
wait_color "$second" green
wait_color "$third" purple
test "$(podman inspect --format '{{.RestartCount}}' "$engine")" = 0
echo 'Root-config integration passed: Git bootstrap, multiple targets, root expansion, workload update and invalid-root preservation.'
