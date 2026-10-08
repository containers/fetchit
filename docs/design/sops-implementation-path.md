# SOPS support for Kube methods

Status: implementation in feature/kube-sops; not shipped until the feature PR merges.
Tracks https://github.com/containers/fetchit/issues/349.
Baseline: main 4828cf6, Podman Go bindings v5.8.8.

## Outcome and scope

Allow users to keep encrypted Kubernetes-style manifests in Git and decrypt them
when FetchIt deploys through the Kube method. Existing configurations continue to
work without decryption. First release supports SOPS YAML with local age keys.
Raw, Quadlet, remote FetchIt configuration, cloud KMS, PGP, and external key
services remain separate follow-ups. Existing Git authentication secrets are
independent of workload secrets.

## Proposed configuration

```yaml
targetConfigs:
- url: https://github.com/example/workloads
  branch: main
  kube:
  - name: application
    targetPath: kube
    schedule: '*/1 * * * *'
    sops:
      ageKeyFile: /run/secrets/fetchit-age-keys
```

An omitted `sops` block retains existing behavior. A present block requires an
absolute, readable age key file; an empty block is a configuration error. The
path is inside the FetchIt container. Mount a host key file or Podman secret
read-only, outside the repository and persistent checkout volume. Never put the
private key in FetchIt's YAML configuration or Git. Preserve optional networks.

Use a dedicated Kube method for encrypted manifests. With SOPS configured,
require encrypted input with authenticated SOPS metadata; do not silently fall
back to plaintext when metadata is missing or decryption fails. Plaintext
manifests can use a separate ordinary Kube method. Publish an authoring example
that encrypts `data` and `stringData`, leaving resource identity visible.

## Decryption adapter and packaging

Use a pinned upstream SOPS executable initially, avoiding a large new SOPS Go
library dependency graph. Build/package the binary for both supported container
architectures; select and verify the release and license during implementation.
Use a fixed executable location in the container, not a command supplied by Git.
Invoke through `exec.CommandContext`, without a shell, using YAML input/output
flags and stdin/stdout. SOPS documents this in-memory interface:
https://getsops.io/docs/usage/advanced/.

Bound encrypted input and plaintext output to 8 MiB per file and total prepared
plaintext to 32 MiB per apply attempt; cap stderr and use a 30-second deadline
per invocation. Exceeding a limit kills the child process and returns a sanitized
error. Do not merely truncate output and attempt deployment. Pass an explicit
minimal environment with only the configured age key source, controlled home,
and required runtime values; do not inherit Git credentials, cloud credentials,
SOPS key-service settings, or ambient age identity sources. Reject non-age
recipient backends in metadata, including key groups, before invoking SOPS.
Keep SOPS integrity/MAC verification enabled. Never log stdout, plaintext,
private keys, raw stderr, or Podman responses that might include secrets.

Before committing to the adapter, prove real pinned-SOPS decryption of a
multi-document Secret+Pod manifest with age, MAC verification, and the minimal
environment. If upstream cannot decrypt that layout as one input, implement
independently encrypted YAML document decoding and bounded reassembly, and
provide authoring commands for that exact format. Do not assume splitting an
already encrypted document preserves its integrity metadata.

## Prepare before applying

Current `Kube.Apply` calls `applyChanges` then `runChanges`; `runChanges` iterates
an unordered map and `kubePodman` stops the old workload before reading the new
file. Add a Kube-specific preparation phase, without altering other methods:

1. Obtain the filtered Git change map and sort it for deterministic processing.
2. Read new content from each changed file and old content from
   `getChangeString`. Keep the repository's original ciphertext untouched.
3. Decrypt all required old/new inputs and validate YAML, supported document
   kinds, resource identities, and existing Pod restrictions. Do not include
   content in parser/validation errors. Preflight configured networks.
4. Only after the entire batch is prepared, apply each operation using prepared
   bytes. A preparation error makes zero stop/play calls.
5. Release plaintext buffers promptly after the attempt. Treat memory clearing
   as best effort; Go does not guarantee removal of every copied secret.

Use `play.KubeWithBody` with a `bytes.Reader` and existing network options for
encrypted inputs. Keep the existing path-based binding for ordinary Kube methods
to avoid changing relative-file/build behavior. Verify and document body-based
limitations, including Containerfile builds and auxiliary file references.
`stopPods` already accepts manifest bytes; feed it prepared old/new plaintext.
Podman 5 supports Secret documents:
https://docs.podman.io/en/v5.6.2/markdown/podman-kube-play.1.html.

## Lifecycle and failure behavior

- Initial deployment: prepare before any Podman mutation.
- Update: prepare both versions before teardown, then stop/play the new version.
- Rename/delete: decrypt old Git content before removing its workloads; never
  require the deleted file to exist in the working tree.
- Rotation: retain old and new age identities until FetchIt has successfully
  replaced or deleted every manifest encrypted to the old identity. A key file
  may contain multiple identities. Changing keys alone does not redeploy an
  unchanged Git manifest; commit a manifest change to rotate workload values.
- Missing/wrong keys, tampered ciphertext, timeouts, invalid YAML, missing
  networks, or oversized input: preserve running workloads and applied-state
  tags; log only a safe failure category and retry on the next schedule.
- Runtime failures after preparation: preserve current retry/tag behavior and
  document possible partial deployment. Preparation is not a Podman transaction.
- Podman secret replacement/removal semantics must be verified on Podman 5
  before shipping. Avoid deleting secrets shared with other methods or workloads.
  Document that workload secret values still reach Podman and its secret store;
  encryption protects Git content, not a compromised deployment host.

## Implementation sequence

1. **Adapter proof:** real age-encrypted Secret+Pod fixture, multi-document/MAC
   proof, bounded execution, and checks of Podman 5 secret rotation/deletion.
2. **Configuration and packaging:** typed optional SOPS settings, exact decode
   tests, pinned SOPS build/install for amd64/arm64, minimal child environment.
3. **Lifecycle refactor:** prepared Kube changes and body-based encrypted play;
   ensure all preparation completes before the first teardown.
4. **Focused tests:** injectable decryptor and Podman calls; assert no mutations
   on any batch preparation failure and no applied-tag advance on failed apply.
5. **Integration and docs:** add a dedicated workflow, complete usage guide,
   release notes, and examples; open the feature PR referencing issue #349.

## Required verification

Unit tests cover omitted/empty/valid settings, limits, cancellation, missing and
wrong keys, tampering, malformed/multi-document YAML, error redaction, plaintext
rejection in encrypted methods, old-content deletion, rename, batch preparation
failure, configured networks, and unchanged ordinary Kube behavior. Real SOPS
fixtures complement adapter mocks. Assert a distinctive secret sentinel never
appears in logs, checkout files, temporary files, or diagnostics.

A dedicated GitHub Actions job must use disposable age keys and a local Git
repository, with encrypted fixtures generated at runtime. On Ubuntu and Fedora
with Podman 5, exercise initial deployment, ciphertext update/secret rotation,
rename, deletion, and failure recovery. Read the deployed secret privately and
compare it without printing the value. On missing/wrong keys and tampering,
assert the old workload still runs and the applied commit has not advanced.
Verify rootful and rootless behavior, and key mounts under Fedora SELinux.
Upload sanitized diagnostics only. Build both image architectures and run the
full existing unit/integration suites and warning-free documentation build.

## User documentation to ship

Add `docs/sops.rst` and link it from `docs/index.rst` and the Kube method guide.
Include age key generation, public recipients and `.sops.yaml`, supported
multi-document authoring commands, Git encryption, key permissions/read-only
Podman mounts, complete FetchIt configuration, networks, update/delete behavior,
rotation overlap, restore/recovery, error troubleshooting, and the exact backend
and body-play limitations. Explain the existing alternative of provisioning
Podman workload secrets outside Git. Mark all proposed configuration above as
unavailable until the feature PR lands.
