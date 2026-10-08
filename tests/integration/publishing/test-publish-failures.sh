#!/usr/bin/env bash
# Fault injection exercises failure paths without publishing to a real registry.
set -euo pipefail
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir "$fixture/bin"
cat > "$fixture/bin/podman" <<'PYTHON'
#!/usr/bin/env python3
import json
import os
from pathlib import Path
import sys

args = sys.argv[1:]
root = Path(os.environ['FETCHIT_FAULT_DIRECTORY'])
case = os.environ['FETCHIT_FAULT_CASE']
with (root / 'calls').open('a') as calls:
    calls.write(json.dumps(args) + '\n')
if args[:2] == ['image', 'inspect']:
    architecture = 'amd64' if args[-1] == 'fixture-amd' else 'arm64'
    print(('windows' if case == 'non-linux' else 'linux') + '/' + architecture)
elif args[0] == 'push':
    if '--digestfile' in args:
        path = Path(args[args.index('--digestfile') + 1])
        path.write_text('sha256:' + ('a' if path.name == 'amd.digest' else 'b') * 64)
elif args[:2] == ['manifest', 'push']:
    path = Path(args[args.index('--digestfile') + 1])
    if case == 'missing':
        pass
    elif case == 'empty':
        path.write_text('')
    elif case == 'malformed':
        path.write_text('not-a-digest')
    elif case == 'unsupported':
        path.write_text('sha512:' + 'c' * 128)
    else:
        path.write_text('sha256:' + 'c' * 64)
elif args[:2] == ['manifest', 'inspect']:
    if args[-1].endswith('@sha256:' + 'c' * 64):
        (root / 'index-inspected').touch()
        print(json.dumps({'manifests': [
            {'platform': {'os': 'linux', 'architecture': architecture}, 'digest': 'sha256:' + digit * 64}
            for architecture, digit in [('amd64', 'a'), ('arm64', 'b')]
        ]}))
    else:
        print('{}')
elif args[:2] not in (['manifest', 'create'], ['manifest', 'add'], ['manifest', 'rm']):
    raise SystemExit('Unexpected fake Podman command: ' + repr(args))
PYTHON
chmod +x "$fixture/bin/podman"
export PATH="$fixture/bin:$PATH"
for case in missing empty malformed unsupported non-linux success; do
  directory="$fixture/$case"
  mkdir "$directory"
  status=0
  FETCHIT_FAULT_CASE="$case" FETCHIT_FAULT_DIRECTORY="$directory" \
    bash scripts/publish-platform-index.sh example.invalid/test:latest fixture-amd fixture-arm \
      > "$directory/output" 2>&1 || status=$?
  if [[ "$case" == success ]]; then
    test "$status" = 0
    test -f "$directory/index-inspected"
  else
    test "$status" != 0
    test ! -e "$directory/index-inspected"
    case "$case" in
      missing|empty) grep -q 'Missing or empty pushed index digest' "$directory/output" ;;
      malformed|unsupported) grep -q 'Invalid pushed index digest' "$directory/output" ;;
    esac
  fi
  python3 - "$directory/calls" "$case" <<'PYTHON'
import json
import sys
calls = [json.loads(line) for line in open(sys.argv[1])]
inspections = [call for call in calls if call[:2] == ['manifest', 'inspect']]
if sys.argv[2] == 'non-linux':
    assert not any(call[0] == 'push' or call[:2] == ['manifest', 'push'] for call in calls), calls
    assert not inspections, inspections
else:
    # Only the two child preflights may run when the index digest is invalid.
    assert len(inspections) == (3 if sys.argv[2] == 'success' else 2), inspections
PYTHON
  echo "Publishing fault case passed: $case"
done
