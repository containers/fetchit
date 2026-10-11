#!/usr/bin/env bash
# Exercise the production engine with a private Git fixture and real Podman.
set -euo pipefail
engine_image=${FETCHIT_TEST_ENGINE_IMAGE:-quay.io/fetchit/fetchit-amd:latest}
sample_image=quay.io/fetchit/fetchit-sample-app:latest
listen=${FETCHIT_TEST_METRICS_ADDR:-127.0.0.1:8080}
endpoint=${FETCHIT_TEST_METRICS_URL:-http://127.0.0.1:8080/metrics}
scratch=$(mktemp -d)
fixture_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/../live-update" && pwd)
engine="fetchit-metrics-$$"
workload="$engine-app"
state="$engine-state"
server_pid=
cleanup() {
  status=$?
  podman logs "$engine" || true
  cat "$scratch/server.log" || true
  podman rm -f "$engine" "$workload" >/dev/null 2>&1 || true
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
git -C "$scratch/seed" config user.name 'FetchIt metrics fixture'
git -C "$scratch/seed" config user.email 'ci@example.invalid'
git -C "$scratch/seed" remote add origin "$scratch/server/live.git"
write_workload() {
  cat > "$scratch/seed/raw/app.json" <<JSON
{"Image":"$sample_image","Name":"$workload","Env":{"APP_COLOR":"$1"}}
JSON
}
publish() {
  git -C "$scratch/seed" add raw
  git -C "$scratch/seed" commit -m "$1"
  git -C "$scratch/seed" push origin main
}
write_workload blue
publish initial
python3 "$fixture_dir/git-server.py" "$scratch/server" "$scratch/port" > "$scratch/server.log" 2>&1 &
server_pid=$!
for ((attempt=0;attempt<50;attempt++)); do
  if [[ -s "$scratch/port" ]]; then break; fi
  kill -0 "$server_pid"
  sleep 0.1
done
port=$(cat "$scratch/port")
cat > "$scratch/config.yaml" <<CONFIG
targetConfigs:
- url: http://127.0.0.1:$port/live.git
  branch: main
  raw:
  - name: private-method-name
    targetPath: raw
    schedule: "*/1 * * * *"
CONFIG
podman run -d --name "$engine" --network host --security-opt label=disable \
  -e FETCHIT_METRICS_ADDR="$listen" -e FETCHIT_HOSTNAME=edge-metrics-test \
  -v "$state:/opt" -v "$scratch/config.yaml:/opt/mount/config.yaml:ro" \
  -v /run/podman/podman.sock:/run/podman/podman.sock "$engine_image"
metric() {
  curl --fail --silent --max-time 3 "$endpoint" |
    awk -v name="$1" '$1==name {print $2}'
}
wait_state() {
  local wanted=$1 deadline=$((SECONDS+150))
  while ((SECONDS<deadline)); do
    if [[ $(podman inspect --format '{{.State.Running}}' "$engine" 2>/dev/null) != true ]]; then return 1; fi
    if [[ $(metric "fetchit_git_methods{hostname=\"edge-metrics-test\",kind=\"raw\",state=\"$wanted\"}" || true) == 1 ]]; then return 0; fi
    sleep 1
  done
  echo "Timed out waiting for $wanted" >&2
  return 1
}
color() { podman exec "$workload" printenv APP_COLOR; }
wait_state synced
test "$(color)" = blue
output=$(curl --fail --silent "$endpoint")
! [[ "$output" =~ private-method-name|live.git|127.0.0.1|app.json|APP_COLOR ]]
base=${endpoint%/metrics}
test "$(curl -s -o /dev/null -w '%{http_code}' "$base/status")" = 404
test "$(curl -s -o /dev/null -w '%{http_code}' "$base/debug/pprof/")" = 404
test "$(curl -s -X POST -o /dev/null -w '%{http_code}' "$endpoint")" = 405
curl --fail --silent -I "$endpoint" | awk 'tolower($0) ~ /^x-fetchit-hostname: edge-metrics-test/ {found=1} END {exit !found}'
# Repeated scrapes must not invoke reconciliation or recreate the workload.
old_id=$(podman inspect --format '{{.Id}}' "$workload")
for ((i=0;i<30;i++)); do curl --fail --silent "$endpoint" >/dev/null; done
test "$(podman inspect --format '{{.Id}}' "$workload")" = "$old_id"
# Observe an actual scheduled update, without restarting the engine.
write_workload pink
publish update
# State may remain synced until the next scheduled check; wait for the workload.
for ((deadline=SECONDS+150;SECONDS<deadline;)); do
  if [[ $(color 2>/dev/null) == pink ]]; then break; fi
  sleep 1
done
test "$(color)" = pink
wait_state synced
# Malformed workload produces fresh Git knowledge and a failed application.
printf '{invalid-json\n' > "$scratch/seed/raw/app.json"
publish invalid
wait_state out_of_sync
# Source failure must not advance the observed timestamp.
first=$(metric 'fetchit_git_oldest_observation_timestamp_seconds{hostname="edge-metrics-test",kind="raw"}')
touch "$scratch/server/offline"
wait_state unknown
test "$(metric 'fetchit_git_oldest_observation_timestamp_seconds{hostname="edge-metrics-test",kind="raw"}')" = "$first"
write_workload green
publish recover
rm "$scratch/server/offline"
wait_state synced
test "$(color)" = green
second=$(metric 'fetchit_git_oldest_observation_timestamp_seconds{hostname="edge-metrics-test",kind="raw"}')
awk -v first="$first" -v second="$second" 'BEGIN {exit !(second > first)}'
test "$(podman inspect --format '{{.RestartCount}}' "$engine")" = 0
echo 'Metrics integration passed: Git update, apply failure, source failure, recovery, hostname and read-only routes.'
