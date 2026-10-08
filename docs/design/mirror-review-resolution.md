# PR #399 Sourcery review resolutions

- Normalize empty/duplicate sources before selecting failover. A primary-only
  normalized list keeps the legacy single-source timeout path.
- Validate all source transports before using shared credentials. Reject
  authenticated HTTP, unsupported schemes, malformed sources, embedded HTTPS
  credentials, and nonlocal/relative file URLs. The explicitly configured source
  list is the operator's trust allowlist; do not require a second redundant list.
  Per-source credentials remain outside scope and are documented as unsupported.
- Configure go-git's known-hosts callback explicitly for SSH clones. Fetch uses
  the same upstream callback default and configured SSH_KNOWN_HOSTS file. Neither
  uses an insecure callback; the original lack-of-verification implication was
  incorrect, but making clone configuration explicit helps review.
- Use a distinct temporary ref per fetch and compare-and-set branch updates.
  Serialize clone and fetch/checkout across target configs sharing a cache path.
  Concurrency tests run with the race detector in Ubuntu CI.
- Test two usable mirrors with different descendant commits so reversed ordering
  fails, rather than checking only candidate counts.
- Cover malformed/empty sources, transport credential policy, duplicate-only
  selection, absent branches, complete failures and cleanup, rejected histories,
  and unsigned candidates. A source error preserves its cause via Unwrap while
  keeping URLs and credential-bearing transport text out of Error output.
- Log staging-directory and temporary-ref cleanup failures instead of discarding
  them. Extract authentication, clone verification, origin normalization, and
  candidate fetching into focused helpers.
- Keep signature verification within each bounded attempt context.
