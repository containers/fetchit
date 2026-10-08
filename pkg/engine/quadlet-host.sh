# Executed by Fetchit inside a host-administration helper. Arguments are data, never shell code.
# Arguments: parent, runtime (empty for system manager), namespace, old service
# list, desired service list, start flag, restart service list, expected current
# revision, desired revision, configuration ID. Service lists use newlines.
[ "$#" -eq 10 ] || { echo "Expected ten Quadlet helper arguments" >&2; exit 1; }
parent=$1 runtime=$2 namespace=$3 old_services=$4 new_services=$5 start=$6 restart_units=$7 current=$8 desired_revision=$9 config_id=${10}
host=${FETCHIT_QUADLET_HOST_ROOT:-/host}
control="$parent/.${namespace}"
live="$parent/systemd/$namespace"
if [ -n "$runtime" ]; then live="$parent/containers/systemd/$namespace"; fi
stage="$control/desired"
fail() { echo "$*" >&2; exit 1; }
hostcmd() { chroot "$host" /usr/bin/env PATH=/usr/sbin:/usr/bin:/sbin:/bin "$@"; }
systemctl_cmd() {
 if [ -n "$runtime" ]; then hostcmd XDG_RUNTIME_DIR="$runtime" systemctl --user "$@";
 else hostcmd systemctl "$@"; fi
}
# Reject symlinked managed directories before accessing persistent control state.
for dir in "$control" "$parent/systemd" "$parent/containers" "$parent/containers/systemd" "$live"; do
 [ ! -L "$host$dir" ] || fail "Managed directory is a symlink: $dir"
done
mkdir -p "$host$control"
# Serialize simultaneous runs and preserve incomplete operations for retry.
exec 9>"$host$control/lock"
flock -x 9
# Compare-and-set the applied receipt under the lock. A stale process must not
# replace a newer commit. A new settings identity may initialize its own Git tag.
if [ -f "$host$control/receipt" ]; then
 installed_revision=$(sed -n '1p' "$host$control/receipt")
 installed_config=$(sed -n '2p' "$host$control/receipt")
 if [ "$installed_revision" != "$current" ] && [ "$installed_revision" != "$desired_revision" ]; then
  if [ "$current" != 0000000000000000000000000000000000000000 ] || [ "$installed_config" = "$config_id" ]; then
   fail "Stale Quadlet plan: host revision $installed_revision differs from expected $current"
  fi
 fi
 if [ "$installed_revision" = "$desired_revision" ] && [ "$installed_config" = "$config_id" ] && [ ! -f "$host$control/pending" ]; then
  echo 'Quadlet receipt is current; no changes required'
  exit 0
 fi
fi

version=$(hostcmd podman --version)
case "$version" in *'version 5.'*) ;; *) fail "Quadlet requires host Podman 5.7 or newer within major 5: $version";; esac
minor=$(printf '%s' "$version" | sed -n 's/.*version 5\.\([0-9]*\).*/\1/p')
[ "$minor" -ge 7 ] || fail "Quadlet requires Podman >=5.7 and <6"
[ "$(hostcmd stat -fc %T /sys/fs/cgroup)" = cgroup2fs ] || fail "Quadlet requires cgroup v2"
generator=/usr/lib/systemd/system-generators/podman-system-generator
[ -x "$host$generator" ] || fail "Host Quadlet generator missing: $generator"
systemctl_cmd show --property=Version >/dev/null
rm -rf "$host$stage"
mkdir -p "$host$stage"
cp -a --no-preserve=context,xattr "${FETCHIT_QUADLET_BUNDLE:-/tmp/quadlet-bundle}/." "$host$stage/"
if [ -n "$runtime" ]; then
 hostcmd QUADLET_UNIT_DIRS="$stage" "$generator" --user --dryrun >"$host$control/validation.log" 2>&1 || { cat "$host$control/validation.log"; fail 'Quadlet validation failed'; }
else
 hostcmd QUADLET_UNIT_DIRS="$stage" "$generator" --dryrun >"$host$control/validation.log" 2>&1 || { cat "$host$control/validation.log"; fail 'Quadlet validation failed'; }
fi
# Podman 5 dry-run output delimits units as ---web.service--- (no spaces).
# The generator can skip invalid sources: require output for every expected unit.
printf '%s\n' "$new_services" | while IFS= read -r unit; do
 [ -n "$unit" ] || continue
 grep -F -- "---$unit---" "$host$control/validation.log" >/dev/null || { cat "$host$control/validation.log"; fail "Generator did not produce $unit"; }
 # Do not replace units managed outside this bundle.
 source=$(systemctl_cmd show "$unit" --property=SourcePath --value)
 fragment=$(systemctl_cmd show "$unit" --property=FragmentPath --value)
 case "$source" in "$live/"*) ;; '') [ -z "$fragment" ] || fail "Service already exists outside bundle: $unit" ;; *) fail "Service belongs to another bundle: $unit ($source)";; esac
done
# pending contains names from a prior incomplete application, even if Git changed.
{
 printf '%s\n' "$old_services"
 [ ! -f "$host$control/installed" ] || cat "$host$control/installed"
 [ ! -f "$host$control/pending" ] || cat "$host$control/pending"
} | sed '/^$/d' | sort -u >"$host$control/previous"
{ cat "$host$control/previous"; printf '%s\n' "$new_services"; } | sed '/^$/d' | sort -u >"$host$control/pending.tmp"
mv "$host$control/pending.tmp" "$host$control/pending"
while IFS= read -r unit; do
 if ! printf '%s\n' "$new_services" | grep -Fx -- "$unit" >/dev/null; then
  load=$(systemctl_cmd show "$unit" --property=LoadState --value)
  if [ "$load" != not-found ]; then
   source=$(systemctl_cmd show "$unit" --property=SourcePath --value)
   case "$source" in "$live/"*) ;; *) fail "Refusing to stop unit outside bundle: $unit ($source)";; esac
   systemctl_cmd stop "$unit"
  fi
 fi
done <"$host$control/previous"
# Replace only the directory owned by this method, retaining relative paths.
mkdir -p "$host$live"
find "$host$live" -mindepth 1 -maxdepth 1 -exec rm -rf -- {} +
cp -a --no-preserve=context,xattr "$host$stage/." "$host$live/"
systemctl_cmd daemon-reload
if [ "$start" = true ]; then
 printf '%s\n' "$new_services" | while IFS= read -r unit; do
  [ -n "$unit" ] || continue
  if printf '%s\n' "$restart_units" | grep -Fx -- "$unit" >/dev/null; then systemctl_cmd restart "$unit"; else systemctl_cmd start "$unit"; fi
 done
fi
printf '%s\n' "$new_services" >"$host$control/installed.tmp"
mv "$host$control/installed.tmp" "$host$control/installed"
printf '%s\n%s\n' "$desired_revision" "$config_id" >"$host$control/receipt.tmp"
mv "$host$control/receipt.tmp" "$host$control/receipt"
rm -f "$host$control/pending"
echo 'Quadlet bundle reconciled successfully'
