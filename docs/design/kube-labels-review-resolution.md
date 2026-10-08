# Kube label review resolution

PR #402 implements discovery labels for issue #100.

Addressed:
- Ordinary deletion tests use `kubePodman`'s delete path and verify the labeled
  Pod is absent. Runtime assertions check labels after ordinary recreation and
  after SOPS recovery, key rotation, and rename.
- `KubeManifestError` retains the underlying decode/encode/finalize cause via
  `Unwrap`, with an operation classification available through `errors.As`.
  Its message is static so parser details cannot expose decrypted SOPS values
  through normal error logging. Failed encoding buffers are cleared.
- Encoder finalization errors are checked before returning the output.
- Owner hashing and per-document label traversal use focused helpers. A fixed
  array of strings cannot cause JSON serialization to fail; that constraint is
  documented rather than adding an unreachable error branch.
- Related constants are grouped. The guide has workload-label and manifest
  subsections, explains creation/recreation, and assigns the owner filter from
  Pod inspection.
- The runtime fixture uses `podman pod ps` without `-a`; Pods are already all
  listed by default, unlike `podman ps` for containers.

Deferred ownership enforcement:
- Discovery labels do not change the existing name-based deletion/replacement
  semantics. The user guide and PR description state this explicitly.
- Enforcement needs a reviewed migration policy for unlabeled workloads,
  configuration identity changes, and ownership of Secrets/volumes torn down by
  the same multi-document kube operation. Checking only a Pod label would leave
  those other destructive operations unprotected. Labels are user-editable and
  must not be presented as a security boundary.
- This PR therefore implements the issue's label acceptance criterion while
  keeping that broader lifecycle policy as a separate follow-up.
