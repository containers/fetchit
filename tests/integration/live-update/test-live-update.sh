#!/usr/bin/env bash
# Test the real engine's scheduled Git polling using only job-local resources.
set -euo pipefail
engine_image=${FETCHIT_TEST_ENGINE_IMAGE:-quay.io/fetchit/fetchit-amd:latest}
sample_image=quay.io/fetchit/fetchit-sample-app:latest
scratch=$(mktemp -d)
fixture_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
engine="fetchit-live-update-$$"
changing="$engine-changing"
stable="$engine-stable"
state="$engine-state"
server_pid=
cleanup() {
  status=$?
  podman logs "$engine" || true
  cat "$scratch/server.log" || true
  podman rm -f "$engine" "$changing" "$stable" >/dev/null 2>&1 || true
  podman volume rm "$state" >/dev/null 2>&1 || true
  if [[ -n "$server_pid" ]]; then kill "$server_pid" 2>/dev/null || true; wait "$server_pid" 2>/dev/null || true; fi
  rm -rf "$scratch"
  exit "$status"
}
trap cleanup EXIT
podman image exists "$engine_image"
podman image exists "$sample_image"
mkdir -p "$scratch/server" "$scratch/seed/raw"
git init --bare --initial-branch=main "$scratch/server/live.git"
git -C "$scratch/server/live.git" config http.receivepack false
git init --initial-branch=main "$scratch/seed"
git -C "$scratch/seed" config user.name 'FetchIt CI fixture'
git -C "$scratch/seed" config user.email 'ci@example.invalid'
git -C "$scratch/seed" remote add origin "$scratch/server/live.git"
write_workload() {
  local name=$1 color=$2 destination=$3
  cat > "$destination" <<EOF
{"Image":"$sample_image","Name":"$name","Env":{"APP_COLOR":"$color"}}
EOF
}
write_workload "$changing" blue "$scratch/seed/raw/changing.json"
write_workload "$stable" green "$scratch/seed/raw/stable.json"
git -C "$scratch/seed" add raw
git -C "$scratch/seed" commit -m 'Initial workload definitions'
git -C "$scratch/seed" push origin main
python3 "$fixture_dir/git-server.py" "$scratch/server" "$scratch/port" > "$scratch/server.log" 2>&1 &
server_pid=$!
for ((attempt = 0; attempt < 50; attempt++)); do
  if [[ -s "$scratch/port" ]]; then break; fi
  kill -0 "$server_pid"
  sleep 0.1
done
port=$(cat "$scratch/port")
# Verify the same smart HTTP transport that FetchIt will use before starting it.
git ls-remote "http://127.0.0.1:$port/live.git" refs/heads/main
cat > "$scratch/config.yaml" <<EOF
targetConfigs:
- url: http://127.0.0.1:$port/live.git
  branch: main
  raw:
  - name: live-update
    targetPath: raw
    schedule: "*/1 * * * *"
EOF
podman run -d --name "$engine" --network host --security-opt label=disable \
  -v "$state:/opt" \
  -v "$scratch/config.yaml:/opt/mount/config.yaml:ro" \
  -v /run/podman/podman.sock:/run/podman/podman.sock \
  "$engine_image"
wait_color() {
  local name=$1 color=$2 deadline=$((SECONDS + 150))
  while (( SECONDS < deadline )); do
    if [[ $(podman inspect --format '{{.State.Running}}' "$engine" 2>/dev/null) != true ]]; then
      echo 'FetchIt exited while waiting for reconciliation' >&2
      return 1
    fi
    if [[ $(podman inspect --format '{{.State.Running}}' "$name" 2>/dev/null) == true ]] &&
       [[ $(podman exec "$name" printenv APP_COLOR 2>/dev/null) == "$color" ]]; then return 0; fi
    sleep 2
  done
  echo "Timed out waiting for $name to reach APP_COLOR=$color" >&2
  return 1
}
wait_color "$changing" blue
wait_color "$stable" green
old_id=$(podman inspect --format '{{.Id}}' "$changing")
stable_id=$(podman inspect --format '{{.Id}}' "$stable")
engine_id=$(podman inspect --format '{{.Id}}' "$engine")
write_workload "$changing" pink "$scratch/seed/raw/changing.json"
git -C "$scratch/seed" add raw/changing.json
git -C "$scratch/seed" commit -m 'Update one running workload'
# This push goes only to the temporary bare repository, never to GitHub.
git -C "$scratch/seed" push origin main
wait_color "$changing" pink
wait_color "$stable" green
test "$(podman inspect --format '{{.Id}}' "$changing")" != "$old_id"
test "$(podman inspect --format '{{.Id}}' "$stable")" = "$stable_id"
test "$(podman inspect --format '{{.Id}}' "$engine")" = "$engine_id"
test "$(podman inspect --format '{{.RestartCount}}' "$engine")" = 0
echo 'Scheduled Git update replaced only the changed workload without restarting FetchIt.'
