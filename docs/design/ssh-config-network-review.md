# PR #398 Sourcery review resolutions

- Stage the config and backup before publishing either. Publish the new backup
  only after config replacement succeeds. A failed backup rename rolls the config
  back; tests inject both rename failures and assert preservation and error types.
  Failed rollback is explicitly reported; this is not a two-file crash transaction.
- Inspect optional Raw/Kube networks before any workload teardown. Unit tests
  exercise the preflight path before old workload parsing/teardown. Network races
  and other runtime failures remain possible and are documented.
- Network-only config changes require a tracked workload commit; the configuration
  workflow explicitly documents this action, as the review permits. This retains
  existing Git-driven reconciliation semantics.
- Centralize CI directory preparation in a composite action using directory mode
  0700 and config mode 0600. Substitute the test token only after secure staging.
- Scan SSH keys into a candidate file, print fingerprints, compare against the
  provider's published keys, then install the verified file into known_hosts.
- Separate YAML mapping validation from exact configuration decoding; organize
  imports consistently and wrap filesystem errors with `%w`.
- The repeated `bytes`-import findings are false: the import already exists and
  the full local suite, Linux vet, and CI build/Fedora tests compile this code.
- Fix the SSH fixture's Git commands to use `/tmp` as the working directory:
  Ubuntu's private runner checkout is not accessible to the disposable Git user.
