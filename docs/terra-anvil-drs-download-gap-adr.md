# ADR 0004 / GitHub Issue: Support Terra/AnVIL DRS downloads in `git-drs`

## Status

Proposed

## Issue type

Architecture Decision Record / feature gap analysis

## Download

[Download this ADR as Markdown](./terra-anvil-drs-download-gap-adr.md)

## Related documents

- [Terra/AnVIL TDD acceptance tests](./terra-anvil-tdd-tests.md)

## Short answer: Terra DRS service-info endpoint

Yes. Terra Data Repository exposes the GA4GH DRS service-info endpoint at:

```text
https://data.terra.bio/ga4gh/drs/v1/service-info
```

For `git-drs` Terra health checks, `https://data.terra.bio` is the Terra DRS service base URL and `git drs ping` derives `/ga4gh/drs/v1/service-info` from that base. Terra also uses DRSHub for DRS URI resolution; DRSHub URLs such as `https://drshub.dsde-<env>.broadinstitute.org/api/v4/drs/resolve` are resolver API URLs, not the GA4GH DRS service-info endpoint that `git drs ping` should use.

## Summary

`git-drs` already has most of the repository and hydration machinery needed for Terra/AnVIL, so the remaining work should be scoped as an incremental provider/identity extension rather than a wholesale redesign. The most direct path is to add a `terra` remote mode, wire Terra authentication and provider resolution at remote setup time, and then close the two compatibility gaps that are currently not well-defined for Terra references: SHA256-shaped local identity and direct DRS URL pointer identity. A manifest bootstrap workflow remains useful, but it can build on the same lower-level `add-ref`/resolver work instead of being treated as a separate large subsystem.

The Terra remote endpoint used for service-health checks should be the Terra DRS service base URL, for example `https://data.terra.bio` in production. That base URL supports the GA4GH DRS service-info route at `/ga4gh/drs/v1/service-info`. DRSHub remains relevant for resolving Terra DRS URIs into access information, but it is a resolver service rather than the service-info host for `git drs ping`.

## Background

Gregor project users want a Git repository that stores project structure, manifests, analysis code, and `git-drs` pointer files while leaving controlled-access payload bytes on AnVIL. Users should be able to clone normal Git history, review which data objects are referenced, authenticate through AnVIL/Terra access controls, and hydrate only selected paths with commands such as:

```bash
git drs pull -I "data/cohort-a/**"
```

Terra/AnVIL DRS downloads typically start from stable DRS URIs and use Terra-compatible authentication and resolution tooling to obtain authorized access URLs. A repository bootstrap workflow also needs to query AnVIL DRS metadata and generate pointer files and manifests deterministically.

## Current state

`git-drs` already provides several useful building blocks:

- Git-compatible pointer workflows.
- `git drs track` for tracked data path patterns.
- `git drs pull -I <pattern>` for selective hydration.
- `git drs add-ref <drs_uri> <dst path>` as an initial single-object reference workflow.
- Gen3/Syfon remote configuration.
- Local metadata and cache plumbing for DRS-backed objects.

However, these pieces do not yet compose into a complete Terra/AnVIL workflow.

## Decision

Add first-class Terra/AnVIL DRS support as an incremental extension of the
existing remote and reference workflows. The implemented CLI uses the unified
`git drs remote add [name] terra ...` form so authentication and provider
behavior are known at remote configuration time. Once a pointer or reference
is associated with a Terra remote, `git-drs` can use a Terra-aware resolver to
translate the DRS URI into downloadable access URLs. The main unresolved
design decision is how to represent DRS URL identity in pointer files while
preserving the existing SHA256-oriented cache and compatibility assumptions.

## Goals

- Allow repositories to store stable Terra/AnVIL DRS references in Git without committing payload bytes.
- Support selective hydration using existing include-filter semantics.
- Support Terra/AnVIL authentication mechanisms such as Google Application Default Credentials or bearer tokens.
- Resolve DRS URIs to authorized access URLs using GA4GH DRS and/or Terra/Martha-compatible resolution.
- Generate deterministic pointer files from manifests or workspace table exports.
- Preserve reviewability of data-reference changes in pull requests.
- Report metadata conflicts, missing checksums, inaccessible DRS IDs, and path collisions clearly.

## Non-goals

- Bypass AnVIL authentication or authorization.
- Mirror Gregor payload bytes into Git or unmanaged buckets.
- Replace Terra workspace data management.
- Edit AnVIL source metadata from a Git repository.
- Require all Terra/AnVIL DRS records to have SHA256 checksums.

## Team feedback incorporated

This ADR is revised to reflect team feedback that Terra support should be smaller than a broad re-architecture. The preferred shape is:

- add a `terra` remote mode and wire auth/provider behavior there;
- extend `add-ref` or add a related reference command so callers can create Terra-backed DRS references explicitly;
- resolve Terra references through a remote-aware DRS resolver;
- focus design discussion on SHA256 compatibility and DRS URL pointer compatibility;
- keep manifest/bootstrap support as a batch layer over the reference primitive.

The ADR also captures an open pointer-format question raised in review: whether `git-drs` should recognize a `git-drs`-specific pointer file whose `oid` is a DRS URI rather than a Git LFS-style SHA256 value, for example:

```text
version https://calypr.github.io/spec/v1
oid drs://cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c
size 11305017366
```

That pointer shape is attractive for reviewability and direct DRS compatibility, but it needs an explicit cache-key strategy because existing `git-drs` code relies on SHA256-shaped OIDs in several places.

## Gap analysis

### Gap 1: Missing Terra/AnVIL remote type

Current remote configuration is centered on Gen3/Syfon remotes with an `organization/project` scope. Team feedback suggests that a `terra` remote mode is the cleanest and smallest enabling feature: auth, provider selection, and resolver behavior can all be wired when the remote is added.

Example target CLI:

```bash
git drs remote add anvil terra --checkout hydrate
```

The `terra` preset pins the production endpoint, `google-adc` authentication,
and read-only provider behavior.

### Gap 2: Terra references need a remote-aware `add-ref` path

Similar reference-building behavior already exists in `add-url`, and `add-ref` already has the right conceptual role for existing DRS objects. The missing piece is a Terra-aware reference flow where the command knows which remote type owns the reference and can call the matching resolver.

Possible target command shapes:

```bash
git drs add-ref --remote anvil drs://example/object data/object.cram
git drs add-ref --remote-type terra drs://example/object data/object.cram
```

Required behavior:

1. Resolve the DRS URI using the selected remote.
2. Create a pointer and local metadata without downloading payload bytes.
3. Preserve the DRS URI as canonical remote identity.
4. Store or derive whatever SHA256-compatible local identity `git-drs` needs for cache compatibility.

### Gap 3: DRS URI pointer compatibility is not well defined

The existing design still has code paths where local pointer identity is treated as `oid sha256:<64hex>`. Terra/AnVIL can work with that constraint if the DRS URI is stored as first-class metadata and a deterministic SHA256-shaped local cache key is derived from it. However, review feedback also proposes a `git-drs`-specific pointer format where the pointer `oid` itself is a DRS URI.

Option A: keep Git LFS-shaped pointers and store DRS URI in sidecar/local metadata.

```text
version https://git-lfs.github.com/spec/v1
oid sha256:<derived-local-oid>
size <size>
```

Option B: add a `git-drs` pointer spec that accepts DRS URI OIDs directly.

```text
version https://calypr.github.io/spec/v1
oid drs://example/object
size <size>
```

Option A is likely lower-risk because it preserves current SHA256 assumptions. Option B is more reviewable and direct, but requires parser, cache fanout, inventory, and hydration updates so DRS URI OIDs can be normalized into safe local cache keys.

### Gap 4: SHA256 compatibility is still non-negotiable internally

Review feedback notes that SHA256 compatibility remains non-negotiable in current `git-drs`. Terra/AnVIL records may still lack SHA256 checksums or provide different checksum types, so the design should preserve a SHA256-shaped local/cache identity even when that identity is derived from the DRS URI rather than from payload content.

Required behavior:

- DRS URI is required.
- Size should be captured when available.
- SHA256 should be captured when available.
- Missing checksum should be reported as a warning or policy-controlled condition, not always a hard failure.

### Gap 5: Missing Terra/Martha-compatible resolver

Terra downloads often require a resolver layer that can turn a DRS URI into an authorized signed URL. `git-drs` needs a provider abstraction with Terra/AnVIL implementation.

Suggested interface:

```go
type DRSResolver interface {
    GetObject(ctx context.Context, drsURI string) (*DRSObject, error)
    GetAccessURL(ctx context.Context, drsURI string, accessIDOrType string) (*AccessURL, error)
}
```

Expected implementations:

- `syfonResolver`
- `gen3Resolver`
- `ga4ghResolver`
- `terraResolver`

### Gap 6: Manifest bootstrap/import should be a batch wrapper over reference creation

`git-drs` still needs a batch utility to initialize or refresh repositories from authoritative AnVIL data sources, but this should be implemented as a thin wrapper around the Terra-aware reference primitive rather than a separate resolver path.

Target command shape:

```bash
git drs import-drs-manifest \
  --remote anvil \
  --input manifests/gregor-source.tsv \
  --drs-uri-column drs_uri \
  --path-column repo_path \
  --output-manifest manifests/gregor-drs-manifest.tsv \
  --data-root data
```

Required behavior:

- Read TSV/CSV manifests or workspace table exports.
- Resolve each DRS URI.
- Validate metadata.
- Write deterministic pointer files.
- Write or update local `git-drs` metadata.
- Write a deterministic output manifest.
- Support dry-run mode.
- Report additions, changes, deletions, collisions, inaccessible IDs, and metadata conflicts.

### Gap 7: Missing steward/reviewer manifest output

Bootstrap should create a stable, reviewable manifest recording at least:

- repository path;
- DRS URI;
- DRS ID;
- object name;
- size;
- checksum type and value when available;
- access method types;
- source workspace/table/row metadata when available;
- metadata status.

Run-specific details such as timestamps should either be omitted from the committed manifest or isolated in an optional report artifact to preserve idempotency.

### Gap 8: Current scope model does not match Terra discovery

Terra/AnVIL source metadata may come from workspace tables, Data Repository Service snapshots, manifests, or other project-specific exports. The canonical scope for hydration should be the DRS URI stored in metadata, not a Gen3-style `organization/project` plus bucket mapping.

### Gap 9: Access method selection needs policy

Terra/AnVIL DRS objects may expose multiple access methods. `git-drs` should support explicit and default access preferences.

Example target flags:

```bash
# automatic selection is the default
git drs pull
git drs pull --access-method https
git drs pull --access-method gs
```

### Gap 10: Authentication UX is not Terra-native

Terra users should be able to configure auth using expected local mechanisms.

Target options:

```bash
--auth google-adc
--auth gcloud
--auth bearer-token
```

Error messages should distinguish missing credentials, expired credentials, 401 unauthorized, 403 forbidden, inaccessible controlled-access records, and expired signed URLs.

### Gap 11: Need read-only remote mode

Gregor/AnVIL repositories reference existing controlled-access data. They should not accidentally trigger payload upload or DRS registration.

Target behavior:

```bash
git drs remote add anvil terra --checkout hydrate
```

In read-only mode:

- `git drs pull` works.
- manifest import/bootstrap works.
- upload/register push behavior is disabled or fails with a clear message.

### Gap 12: Missing Terra/AnVIL documentation

Add documentation covering:

- required credentials;
- remote setup;
- manifest input format;
- bootstrap workflow;
- refresh workflow;
- selective hydration;
- missing checksum policy;
- controlled-access troubleshooting;
- common 401/403 causes.

## Proposed implementation plan

### Phase 1: Terra remote and resolver

- Add Terra support to the unified `git drs remote add [name] terra ...` path.
- Wire Terra auth/provider configuration at remote setup time.
- Add a Terra-aware resolver used by reference creation and hydration.
- Add provider-specific diagnostics for authorization failures.

### Phase 2: Reference creation and pointer identity

- Extend `add-ref` or add a related command to create Terra-backed references.
- Persist the DRS URI as canonical remote identity.
- Preserve a SHA256-shaped internal/local cache identity, either from a real checksum or a deterministic DRS URI-derived key.
- Decide whether to support direct `oid drs://...` `git-drs` pointer files in addition to Git LFS-shaped pointers.

### Phase 3: Manifest bootstrap/import

- Add `git drs import-drs-manifest` or equivalent as a batch wrapper over Terra-aware reference creation.
- Generate pointer files without downloading payload bytes.
- Generate deterministic output manifests.
- Add dry-run, conflict reporting, and deletion policy options.

### Phase 4: Documentation and acceptance tests

- Add `docs/terra-anvil.md`.
- Add unit tests for DRS-URI-derived local OIDs.
- Add resolver tests for mocked GA4GH/Terra DRS responses.
- Add integration-style tests for manifest import and selective hydration.

## Acceptance criteria

- A maintainer can configure a Terra/AnVIL read-only remote without Gen3/Syfon bucket mapping.
- A bootstrap/import command can generate pointer files from a representative AnVIL manifest without downloading payload bytes.
- Generated pointer metadata includes canonical DRS URIs.
- Missing SHA256 checksums are handled according to documented policy.
- Rerunning bootstrap with unchanged AnVIL metadata produces no Git diff.
- `git drs pull -I <path-or-pattern>` hydrates selected Terra/AnVIL pointer files after authentication.
- 401/403 and inaccessible DRS records produce actionable error messages.
- Documentation explains credentials, bootstrap inputs, outputs, refresh workflow, and troubleshooting.

## Open questions

- The canonical command is the unified `git drs remote add [name] terra`
  preset form; the earlier provider-subcommand alternatives are superseded.
- Which AnVIL/Terra endpoint should be treated as canonical for Gregor DRS discovery?
- Should Martha/Terra resolution be implemented directly or invoked through Terra Notebook Utils behavior?
- What should the default policy be for records that lack checksums?
- Should deleted or withdrawn AnVIL records remove pointers automatically, create tombstones, or require explicit maintainer action?
- Should bootstrap/import live in core `git-drs` or as a project-local Gregor script first?

## Priority

High for Terra/AnVIL adoption.

## Labels

- `adr`
- `enhancement`
- `terra`
- `anvil`
- `drs`
- `bootstrap`
- `controlled-access`
