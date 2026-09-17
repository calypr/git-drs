# Syfon client duplication audit contract

The behavior contract has two invariants.

1. Git-visible behavior and persisted Git, LFS, DRS, credential, and pointer formats do not change.
2. Network requests preserve method, path, authentication, scope, payload, retry, and error semantics unless the Syfon client is a strict behavior-preserving superset.

The audit is organized by the upstream Syfon client symbol. Each worker inventories every exported production symbol in its assigned upstream packages, then searches all git-drs production code for equivalent logic.

Use these relations only:

- `exact`: local and upstream observable behavior match.
- `syfon_superset`: upstream preserves local behavior and adds compatible behavior.
- `local_policy_wrapper`: local code adds Git-specific policy around an upstream operation.
- `incompatible`: similar purpose but different observable behavior.
- `no_match`: no local equivalent.

Only `exact` and `syfon_superset` rows may recommend replacement. A `local_policy_wrapper` row must split generic mechanism from local policy before recommending any deletion.

Each report uses this TSV schema:

`upstream_package\tupstream_symbol\tlocal_symbols\trelation\taction\tremovable_loc\tbehavior_evidence\tcallsite_evidence\tpin\tconfidence`

Rules:

- Read implementations, not names or comments alone.
- Search production and test call sites with `rg`.
- Check generated types and request paths when comparing API wrappers.
- Count disjoint removable lines once. Do not count imports or shared helpers until the edit proves they disappear.
- Preserve public git-drs APIs unless the user explicitly requested a breaking change.
- Preserve persisted formats even when the upstream helper is stricter or more canonical.
- Cite exact local and upstream `path:line` evidence.
- The discovery wave edits only its own TSV report.
