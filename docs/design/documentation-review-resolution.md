# Documentation review resolution

PR #409 addresses Sourcery feedback on the methods overview and related guides.

| Finding | Resolution |
| --- | --- |
| Rootful example and host access risks | Methods now leads with an inline rootless example. The Quadlet guide explains rootful administrative scope, intended-user path selection, and the helper capability/mount model. Rootful setup remains available for workloads requiring it. |
| Mutable helper image and integrity | Quadlet documents an approved digest placeholder, explicit upgrade steps, and external signer/provenance policy. It accurately states that FetchIt does not enforce signatures or an image allowlist and that signatures must not be assumed for development images. Runtime defaults are unchanged by this documentation PR. |
| Concrete compatible version | Source commit 4e7964a is the compatibility baseline for the documented host cleanup features. The publishing guide includes a pinned build and distinguishes Quadlet's helper override from the fixed host-artifact helper reference. No nonexistent release tag or digest is invented. |
| Start default | Set start to true to start after deployment; restart implies start. The methods summary and authoritative Quadlet guide agree. |
| Resource retention ambiguity | The Quadlet guide identifies stopped services, removed source files/containers, retained standard volumes/networks, NetworkDeleteOnStop, custom hooks, and inspection/backup steps. Generator behavior was checked against Podman 5.8 documentation and the v5.8.8 Go generator. |
| Duplicate operational details | Keep inline rootless usage as requested; consolidate detailed Quadlet operations and retention in quadlet.rst, and rollback/migration/retry rules in lifecycle.rst. |
| Mixed rollback targets | Explicitly state that configurations with unsupported methods are rejected rather than partially rolled back. |
| Ownership persistence | Explain that deleting volumes/journals loses applied Git baselines, pending receipts, and host ownership recovery. |
| Ansible recovery | Keep the existing concrete Ansible configuration and explain idempotent playbooks, compensating changes, and manual restoration. |
| Status access controls | Recommend loopback binding and authenticated TLS proxy/firewall controls for remote monitoring; do not claim the runtime rejects public binds or provides authentication. |
| Path tests | Existing TestTrailingSlashTargetGitReconciliation and TestTrailingSlashConfigNormalizationAndIdentity cover Git changes, equivalent spellings, Quadlet ownership, root/empty paths, and absolute-path semantics; both passed again. |
| Sample detail distraction | Remove APP_COLOR/page-color details from method selection; retain them in sample documentation. |

Validation: strict Sphinx build, all 27 YAML examples parsed, targeted normalization tests, and git diff checks passed.
