#!/usr/bin/env bash
# Run on a disposable Linux runner with sudo, Git, OpenSSH, and rootful Podman.
set -euo pipefail
image=${FETCHIT_TEST_IMAGE:-quay.io/fetchit/fetchit-amd:latest}
fixture=$(mktemp -d /tmp/fetchit-ssh.XXXXXX)
port=22222
cleanup() {
  sudo podman logs fetchit-ssh 2>/dev/null || true
  sudo cat "$fixture/sshd.log" 2>/dev/null || true
  sudo podman rm -f fetchit-ssh 2>/dev/null || true
  sudo podman volume rm fetchit-volume 2>/dev/null || true
  sudo podman rm -f ssh-network-raw 2>/dev/null || true
  sudo podman pod rm -f ssh-network-pod 2>/dev/null || true
  sudo podman network rm ssh-front ssh-back 2>/dev/null || true
  if [[ -f "$fixture/sshd.pid" ]]; then sudo kill "$(sudo cat "$fixture/sshd.pid")" 2>/dev/null || true; fi
  sudo rm -rf "$fixture"
}
trap cleanup EXIT
chmod 755 "$fixture"
sudo useradd --create-home --shell /bin/bash git
# Unlock the disposable account; passwords are disabled in sshd_config.
printf 'git:temporary-ci-only\n' | sudo chpasswd
mkdir -p "$fixture/config/.ssh" "$fixture/work/files" "$fixture/work/raw" "$fixture/work/kube" "$fixture/output"
sudo podman network create ssh-front
sudo podman network create ssh-back
sudo podman build -t quay.io/fetchit/fetchit-sample-app:latest -f examples/sample-app/Containerfile examples/sample-app
printf '{"Image":"quay.io/fetchit/fetchit-sample-app:latest","Name":"ssh-network-raw"}\n' > "$fixture/work/raw/container.json"
sed -e 's/colors_pod/ssh-network-pod/g' -e 's/colors-kubeplay/ssh-network-kube/g' -e '/hostPort:/d' examples/kube/3-example.yaml > "$fixture/work/kube/pod.yaml"
ssh-keygen -q -t ed25519 -N '' -f "$fixture/config/.ssh/id_ed25519"
ssh-keygen -q -t ed25519 -N '' -f "$fixture/server_key"
ssh-keygen -q -t ed25519 -N '' -f "$fixture/wrong_key"
sudo install -d -o git -g git -m 700 /home/git/.ssh
sudo install -o git -g git -m 600 "$fixture/config/.ssh/id_ed25519.pub" /home/git/.ssh/authorized_keys
chmod 700 "$fixture/config/.ssh"
git -C "$fixture/work" init -b main
git -C "$fixture/work" config user.name 'SSH integration'
git -C "$fixture/work" config user.email 'ssh-test@example.invalid'
printf 'initial\n' > "$fixture/work/files/hello.txt"
git -C "$fixture/work" add .
git -C "$fixture/work" -c commit.gpgsign=false commit -m initial
git clone --bare "$fixture/work" "$fixture/repo.git"
sudo chown -R git:git "$fixture/repo.git"
sudo -H -u git env -u XDG_CONFIG_HOME git -C /tmp config --global --add safe.directory "$fixture/work"
sudo mkdir -p /run/sshd
cat > "$fixture/sshd_config" <<CONFIG
Port $port
ListenAddress 127.0.0.1
HostKey $fixture/server_key
PidFile $fixture/sshd.pid
AuthorizedKeysFile /home/git/.ssh/authorized_keys
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
AllowUsers git
UsePAM no
StrictModes yes
LogLevel VERBOSE
CONFIG
sudo chown root:root "$fixture"
sudo /usr/sbin/sshd -f "$fixture/sshd_config" -E "$fixture/sshd.log"
cat > "$fixture/config/config.yaml" <<CONFIG
gitAuth:
  ssh: true
  sshKeyFile: id_ed25519
targetConfigs:
- url: ssh://git@127.0.0.1:$port$fixture/repo.git
  branch: main
  filetransfer:
  - name: ssh-integration
    targetPath: files
    destinationDirectory: $fixture/output
    schedule: '* * * * *'
  raw:
  - name: ssh-raw
    targetPath: raw
    networks: [ssh-front, ssh-back]
    schedule: '* * * * *'
  kube:
  - name: ssh-kube
    targetPath: kube
    networks: [ssh-front, ssh-back]
    schedule: '* * * * *'
CONFIG
write_known_host() {
  awk -v address="[127.0.0.1]:$port" '{print address, $1, $2}' "$1" > "$fixture/config/.ssh/known_hosts"
}
start_fetchit() {
  sudo podman run -d --name fetchit-ssh --network host \
    -v fetchit-volume:/opt \
    -v "$fixture/config:/opt/mount:ro" \
    -v /run/podman/podman.sock:/run/podman/podman.sock \
    --security-opt label=disable "$image"
}
wait_for_content() {
  local path=$1 expected=$2
  for ((attempt=0; attempt<90; attempt++)); do
    if [[ -f "$path" ]] && [[ $(cat "$path") == "$expected" ]]; then return; fi
    sleep 2
  done
  echo "Timed out waiting for $path to contain $expected" >&2
  return 1
}
# Verify the SSH fixture itself before exercising FetchIt authentication.
write_known_host "$fixture/server_key.pub"
GIT_SSH_COMMAND="ssh -i $fixture/config/.ssh/id_ed25519 -o IdentitiesOnly=yes -o UserKnownHostsFile=$fixture/config/.ssh/known_hosts -o StrictHostKeyChecking=yes" \
  git ls-remote --heads "ssh://git@127.0.0.1:$port$fixture/repo.git" main | grep -F refs/heads/main

# Reject a valid but untrusted host key before deploying any files.
write_known_host "$fixture/wrong_key.pub"
start_fetchit
rejected=false
for ((attempt=0; attempt<30; attempt++)); do
  if sudo podman logs fetchit-ssh 2>&1 | grep -F 'knownhosts: key mismatch' >/dev/null; then rejected=true; break; fi
  sleep 1
done
if [[ $rejected != true ]] || [[ -e "$fixture/output/hello.txt" ]]; then
  echo 'Untrusted SSH host was not rejected' >&2
  exit 1
fi
sudo podman rm -f fetchit-ssh
sudo podman volume rm fetchit-volume
# Trust the fixture's exact public key, then verify clone and file deployment.
write_known_host "$fixture/server_key.pub"
start_fetchit
wait_for_content "$fixture/output/hello.txt" initial
# A later scheduled fetch must apply updates and new files from the same SSH repo.
printf 'updated\n' > "$fixture/work/files/hello.txt"
printf 'added\n' > "$fixture/work/files/another.txt"
git -C "$fixture/work" add .
git -C "$fixture/work" -c commit.gpgsign=false commit -m update
sudo -H -u git env -u XDG_CONFIG_HOME git -C /tmp --git-dir="$fixture/repo.git" fetch "$fixture/work" main:main
wait_for_content "$fixture/output/hello.txt" updated
wait_for_content "$fixture/output/another.txt" added
# Inspect actual attachments for both Raw and Kube network opt-ins.
for ((attempt=0; attempt<90; attempt++)); do
  if sudo podman container exists ssh-network-raw && sudo podman pod exists ssh-network-pod; then break; fi
  sleep 2
done
infra=$(sudo podman pod inspect ssh-network-pod --format '{{.InfraContainerID}}')
for container in ssh-network-raw "$infra"; do
  sudo podman inspect "$container" --format '{{json .NetworkSettings.Networks}}' | \
    python3 -c 'import json,sys; networks=json.load(sys.stdin); assert set(networks)=={"ssh-front", "ssh-back"}, networks'
done
printf 'SSH trust, clone, fetch, file deployment, and Raw/Kube networking passed\n' 
