# AnVIL/Terra DRS Reference Proof of Concept

## Purpose

This document defines a focused proof of concept (POC) for using `git-drs` with the AnVIL/Terra DRS ecosystem. The POC is a **reference-only workflow**: data already exists behind AnVIL DRS records, and Git stores portable references to that data rather than uploading or copying payload bytes.

The intended end-to-end flow is:

1. An authorized user adds several AnVIL DRS references to a Git repository.
2. The user commits the generated pointer files and pushes the Git repository with ordinary `git push`.
3. A second authorized user clones the repository.
4. The second user authenticates with their own Terra/AnVIL identity and runs `git drs pull` to hydrate the referenced files.

## Scope

The POC includes:

- a read-only Terra remote;
- Google Application Default Credentials (ADC) authentication;
- resolution of AnVIL DRS URIs to authorized access URLs;
- single-reference and manifest-driven batch `add-ref` workflows;
- portable pointer files containing canonical DRS URIs;
- non-secret, repository-level remote configuration;
- selective and complete hydration after a fresh clone;
- size and checksum verification where metadata is available.

The POC does not include:

- uploading payload bytes to AnVIL;
- creating, editing, or deleting AnVIL DRS records;
- copying AnVIL records into Syfon or Gen3;
- destructive remote garbage collection;
- automatic Terra workspace-table synchronization;
- support for every DRS provider or Google authentication mode.

## User Stories

### Data reference author

As an authorized AnVIL user, I want to add one or more existing DRS objects to a Git repository so that I can publish a reviewable, versioned dataset layout without downloading or committing the payload bytes.

Acceptance criteria:

- I can add one reference by DRS URI and destination path.
- I can add many references from a manifest.
- The command validates every DRS URI using my identity.
- Generated pointers contain no payload data, access token, signed URL, or other secret.
- The references can be committed and published with ordinary Git commands.

### Data consumer

As a second authorized AnVIL user, I want to clone the Git repository and hydrate its referenced files using my own identity so that I can reproduce the repository's data layout without receiving the author's credentials or clone-local state.

Acceptance criteria:

- A fresh clone contains portable pointer files.
- The repository supplies non-secret Terra remote configuration.
- I authenticate independently using Google ADC.
- `git drs pull` hydrates all authorized references.
- `git drs pull -I <pattern>` hydrates only selected references.
- Downloaded content is checked against its expected size and checksum when available.

### Unauthorized repository reader

As a person who can read the Git repository but lacks AnVIL data authorization, I may inspect paths and DRS references, but I must not be able to download controlled-access payloads.

Acceptance criteria:

- Cloning Git does not grant data access.
- Pull reports a clear authorization error.
- No committed file or log exposes another user's credential or signed download URL.

### Repository maintainer

As a repository maintainer, I want deterministic pointers, actionable diagnostics, and repeatable tests so that reference changes are reviewable and failures can be distinguished from authorization problems.

## User Experience

### Prerequisites

Both users need:

- Git and `git-drs`;
- access to the Git repository;
- a Google identity authorized for the referenced AnVIL data;
- working Google ADC, initially established with the supported Google authentication tooling.

Each clone configures its remote in the repository-local Git config:

```bash
git drs remote add anvil terra --checkout hydrate
```

Repository-local Git config is authoritative. It is not tracked, and credential material remains in the provider credential store.

### First user: add and publish references

For individual objects:

```bash
git clone <git-repository>
cd <git-repository>

gcloud auth application-default login

git drs add-ref --remote anvil \
  drs://<authority>/<object-id-1> data/sample-1.cram
git drs add-ref --remote anvil \
  drs://<authority>/<object-id-2> data/sample-2.cram

git add .gitattributes data/
git commit -m "Add AnVIL data references"
git push
```

For multiple objects, the preferred POC workflow is a manifest:

```bash
git drs add-ref --remote anvil --manifest references.tsv
git add .gitattributes data/
git commit -m "Add AnVIL data references"
git push
```

The minimum manifest schema is:

```text
drs_uri\tpath
drs://<authority>/<object-id-1>\tdata/sample-1.cram
drs://<authority>/<object-id-2>\tdata/sample-2.cram
```

Optional `size` and `sha256` columns can avoid redundant metadata work, but the command must validate supplied values against authoritative DRS metadata before writing pointers.

### Second user: clone and hydrate

```bash
gcloud auth application-default login

git clone <git-repository>
cd <repository>
git drs pull
```

Selective hydration remains available:

```bash
git drs pull -I "data/*.cram"
```

The second user does not need the first user's `.git/drs` directory, local Git configuration, cached objects, access tokens, or signed URLs.

## Actual Blockers

### 1. Terra runtime does not construct an authenticated data resolver

Terra configuration and runtime selection exist, but the Terra runtime currently creates a context without the Syfon client used by the existing `add-ref` and pull implementations. Terra can therefore be configured and health-checked, but its object-resolution and download path is incomplete.

The POC needs a real AnVIL resolver that authenticates with Google ADC and can retrieve object metadata and authorized access information.

### 2. Data operations are coupled to the Syfon client

Current reference and download paths call `GitContext.Client.DRS()` directly. This assumes every provider can be represented by the Syfon client and fails for the current Terra context. Provider-neutral resolution must replace direct Syfon coupling in `add-ref` and pull.

### 3. `--remote-type` is exposed but does not select behavior

`add-ref` exposes `--remote-type`, but resolver behavior should be selected from the configured remote. The normal interface should be `--remote anvil`; the command loads that remote, observes `type: terra`, and uses the AnVIL resolver automatically.

If `--remote-type` remains temporarily, it must be hidden or deprecated and must be rejected when it conflicts with the selected remote.

### 4. SHA256-bearing references can lose their DRS identity

Current `add-ref` behavior writes a Git LFS-style SHA256 pointer when DRS metadata includes SHA256, while it writes a DRS URI pointer only when SHA256 is unavailable. It stores supplementary metadata under `.git/drs`, which is clone-local and is not transferred by Git.

As a result, a fresh clone may receive a SHA256 and size but not the canonical AnVIL DRS URI needed to resolve the original record. The POC cannot depend on checksum search or the first user's `.git/drs` metadata.

### 5. Pointer identity, cache identity, and content identity are conflated

The current implementation often treats a pointer OID as both a cache key and content SHA256. AnVIL references require three explicit concepts:

- canonical remote identity: the normalized DRS URI;
- local cache identity: a deterministic, filesystem-safe hash derived from the DRS URI;
- content identity: SHA256 or another durable checksum supplied by DRS metadata.

### 6. Current DRS URI resolution is not AnVIL-aware

Deriving `https://<DRS-authority>` and constructing an anonymous generic client is insufficient for authenticated AnVIL resolution. The configured Terra resolver must own authority routing, authentication, object lookup, and access URL retrieval.

### 7. Repository-level remote configuration is missing

Git-local remote configuration is not cloned. Without committed public configuration, every consumer must manually reconstruct the same endpoint, authentication mode, and resolver type. The POC needs a tracked, non-secret configuration format with safe local overrides.

### 8. There is no complete two-user AnVIL acceptance test

Existing focused tests cover pieces of Terra configuration, pointer parsing, and ping behavior. A functional POC requires a clean-clone test using independent user state and real or contract-faithful AnVIL resolution.

## Required Prototype Decision: Commit the Canonical DRS URI

Every Terra/AnVIL reference **must commit the canonical DRS URI in the pointer file**, including when the DRS record supplies a SHA256 checksum.

This is required because:

- `.git/drs` metadata is local and is not pushed or cloned;
- a checksum is content metadata, not necessarily a resolvable AnVIL record identifier;
- the second user must be able to resolve the same record from Git state alone;
- signed access URLs are temporary and must never be committed.

The initial pointer format is:

```text
version https://calypr.github.io/spec/v1
oid drs://<authority>/<object-id>
size <bytes>
sha256 <checksum-if-present>
```

If the pointer parser cannot initially support the optional checksum line, the minimum viable pointer is:

```text
version https://calypr.github.io/spec/v1
oid drs://<authority>/<object-id>
size <bytes>
```

Pull may retrieve the checksum from DRS metadata before download. Under no circumstances may the presence of SHA256 cause Terra `add-ref` to omit the DRS URI from committed state.

For local storage, derive a cache key without replacing canonical identity:

```text
cache_oid = sha256("git-drs-anvil-ref:v1\n" + normalized_drs_uri)
```

## Implementation Plan

### Phase 1: Define a provider-neutral resolver

Introduce a provider-neutral interface used by both `add-ref` and pull:

```go
type Resolver interface {
    GetObject(ctx context.Context, drsURI string) (*ResolvedObject, error)
    GetAccess(ctx context.Context, drsURI, accessID string) (*ResolvedAccess, error)
}

type ResolvedObject struct {
    DRSURI       string
    ID           string
    Name         string
    Size         int64
    Checksums    map[string]string
    AccessMethods []AccessMethod
}

type ResolvedAccess struct {
    URL       string
    Headers   map[string]string
    ExpiresAt *time.Time
}
```

Implement a resolver factory selected by configured remote type:

- `SyfonResolver` for existing Gen3/local behavior;
- `AnVILResolver` for Terra/AnVIL behavior.

Resolver results must not be serialized to tracked files when they contain signed URLs, credentials, or authorization headers.

### Phase 2: Implement AnVIL authentication

Support only `auth: google-adc` for the POC.

The implementation must:

1. Obtain credentials from the current user's ADC environment.
2. Request the scopes required by the supported AnVIL resolver.
3. attach bearer tokens only to trusted resolver endpoints;
4. refresh tokens automatically;
5. redact authorization headers and signed URLs from logs and errors;
6. return distinct errors for absent credentials, expired credentials, and denied authorization.

Tokens, refresh credentials, signed URLs, and request headers must never be written to Git configuration or repository files.

### Phase 3: Implement AnVIL DRS URI resolution

The AnVIL resolver must:

1. Parse and validate `drs://<authority>/<object-id>`.
2. Normalize and preserve the original URI as canonical identity.
3. Route the reference through the configured AnVIL/Terra resolver contract.
4. Retrieve object size, durable checksums, and supported access methods.
5. Resolve an authorized access URL only when a download begins.
6. distinguish not-found, unauthorized, resolver-unavailable, and unsupported-access-method failures.

Authority routing must be implemented inside `AnVILResolver`; it must not assume every URI authority is itself an HTTPS GA4GH DRS endpoint. Exact production endpoints, OAuth scopes, resolver requests, and response contracts must be validated against current authoritative AnVIL/Terra documentation before implementation is considered complete.

### Phase 4: Make Terra pointer files self-contained

Change Terra `add-ref` to always write a DRS pointer, regardless of checksum availability.

The command must:

- preserve the normalized DRS URI;
- store expected size;
- store SHA256 when available;
- avoid writing signed access information;
- write pointers deterministically;
- reject destination paths outside the repository;
- avoid depending on clone-local `.git/drs` metadata for later resolution.

Update parsing, inventory, clean/smudge handling, and status output to preserve the distinction between a DRS URI and a SHA256-shaped OID.

### Phase 5: Separate remote, cache, and content identity

Introduce an explicit identity value:

```go
type ObjectIdentity struct {
    DRSURI        string
    CacheOID      string
    ContentSHA256 string
}
```

Use:

- `DRSURI` for object and access resolution;
- `CacheOID` for local cache fanout paths;
- `ContentSHA256` for integrity validation when available.

Normalize DRS URIs before deriving `CacheOID`, version the derivation scheme, and add golden tests so future normalization changes do not silently orphan cached content.

### Phase 6: Hydrate through the AnVIL resolver

For each DRS pointer, pull must:

1. Parse its canonical DRS URI, expected size, and optional checksum.
2. Derive its local cache OID.
3. reuse an existing cache entry only after validating it;
4. resolve current object metadata with the selected AnVIL remote;
5. request a fresh authorized access URL;
6. download to a temporary file with cancellation and bounded retry;
7. validate size and SHA256 when available;
8. atomically promote verified content into the cache;
9. hydrate the destination path;
10. leave the pointer and cache uncorrupted on failure.

Expired signed URLs must be handled by resolving a new URL rather than persisting or reusing stale access information.

### Phase 7: Add manifest-driven batch `add-ref`

Add:

```bash
git drs add-ref --remote anvil --manifest references.tsv
```

The implementation must:

1. Support required `drs_uri` and `path` columns.
2. Optionally accept `size` and `sha256` as asserted metadata.
3. Validate the entire manifest before modifying the worktree.
4. Reject malformed URIs, absolute paths, repository escapes, duplicate paths, and conflicting entries.
5. Resolve metadata with a small bounded worker pool.
6. compare asserted size/checksum values with authoritative DRS metadata;
7. report all validation failures together where practical;
8. write deterministic pointers after successful validation;
9. support `--dry-run`;
10. print a deterministic result summary.

The batch operation is a wrapper over the same single-reference primitive, not an independent resolution system.

### Phase 8: Configure each clone

Use the remote command to write the single authoritative repository-local Git configuration after cloning.

Initial schema:

```bash
git drs remote add anvil terra --checkout hydrate
```

Configuration rules:

- Git config is the only remote configuration representation;
- credentials remain in environment variables or provider credential stores;
- every clone runs `git drs remote add` before pulling.

## Git Push Semantics for This Prototype

AnVIL data already exists and the configured Terra remote is read-only. Publishing references therefore uses ordinary Git:

```bash
git add .gitattributes data/
git commit -m "Add AnVIL data references"
git push
```

`git push` publishes only:

- pointer files;
- repository paths and Git history;
- `.gitattributes` rules;
- an optional reference manifest when the repository chooses to track it.

It does not:

- upload payload bytes;
- create or update AnVIL DRS records;
- copy records into another DRS service;
- transfer user credentials;
- persist signed download URLs.

`git drs push` against a read-only Terra remote must fail before performing work with an actionable message such as:

```text
remote "anvil" is read-only; publish AnVIL references with ordinary git push
```

Plain `git push` must not attempt to contact AnVIL. Data authorization is evaluated later, independently for each user, when `git drs pull` resolves and downloads the committed references.

## Prototype Acceptance Tests

### 1. Single-reference two-user flow

1. User A authenticates with ADC.
2. User A adds a stable AnVIL DRS reference.
3. The generated file is a pointer containing the canonical DRS URI.
4. User A commits and pushes with ordinary Git.
5. User B clones into a clean environment.
6. The cloned file remains a pointer.
7. User B authenticates independently.
8. User B runs `git drs pull -I <path>`.
9. The file is hydrated and matches expected size and checksum.

### 2. Multiple-reference manifest flow

Import at least ten references, including:

- small and multipart-sized objects;
- nested destination paths;
- records with SHA256;
- a record without SHA256, if the target test data provides one.

Verify deterministic pointers, full pull, selective pull, and an accurate summary.

### 3. Fresh-clone portability

Before User B clones, ensure no User A state is copied, including:

- `.git/drs`;
- `.git/lfs/objects`;
- local Git configuration;
- environment tokens;
- ADC files.

Successful User B hydration proves that committed pointers and repository configuration are sufficient.

### 4. Authorization isolation

A user who can clone Git but cannot access the AnVIL objects must receive a clear authorization error. Confirm that no token, authorization header, or signed URL appears in Git history, pointer content, standard output, standard error, or logs.

### 5. Missing and malformed references

Verify actionable, distinct failures for:

- malformed DRS URI;
- unknown record;
- unsupported authority;
- record without a supported access method;
- duplicate or escaping destination path.

Batch validation should avoid partial worktree modification unless partial behavior is explicitly requested.

### 6. Expired access URL

Simulate or wait for access URL expiry. Pull must resolve a fresh URL and continue without relying on stored access information.

### 7. Integrity failure

Inject incorrect bytes or metadata. Pull must fail verification, delete temporary/corrupt cache content, and leave the worktree pointer intact.

### 8. Interrupted download and retry

Interrupt a download and retry it. The retry must either resume safely or restart cleanly, never treating partial bytes as a valid cache entry.

### 9. Idempotency and cache reuse

After successful hydration, repeated `git drs pull` must not redownload verified content. Two paths referencing the same canonical URI should reuse the same cache entry.

### 10. Read-only push behavior

Verify that:

- ordinary `git push` succeeds without contacting AnVIL;
- `git drs push` against the Terra remote fails clearly and performs no registration, upload, or deletion.

### 11. Repository configuration safety

Verify that repository-local Git configuration:

- is created by `git drs remote add` after a fresh clone;
- is the only source of remote metadata;
- contains credential source identifiers but never credential values.

## Backlog in Priority Order

### P0: required for the two-user POC

1. Introduce the provider-neutral resolver abstraction.
2. Confirm the authoritative AnVIL resolver endpoint, request contract, OAuth scopes, and supported access methods.
3. Implement Google ADC authentication and token refresh.
4. Implement AnVIL object metadata and access resolution.
5. Make Terra `add-ref` always commit the canonical DRS URI.
6. Add explicit DRS URI, cache OID, and content checksum identities.
7. Refactor pull to use `AnVILResolver` rather than a Syfon client.
8. Validate size and SHA256 and atomically manage cache content.
9. Implement and safely load repository-local Git configuration.
10. Reject `git drs push` for read-only Terra remotes.
11. Add a real or contract-faithful two-user clone-and-pull acceptance test.

### P1: required for a usable prototype

1. Add manifest-driven batch `add-ref` and `--dry-run`.
2. Add bounded parallel metadata resolution and download.
3. Add safe retry, cancellation, and expired-URL re-resolution.
4. Add `git drs doctor` checks for ADC, configuration, endpoint health, resolution, authorization, and pointer validity.
5. Improve errors for absent credentials, denied access, missing records, invalid manifests, and integrity failures.
6. Add CI coverage for clean-clone setup, authorization isolation, and repository configuration safety.
7. Publish versioned binaries and a compatibility matrix for the tested AnVIL contract.

### P2: explicitly deferred

1. Uploading payloads to AnVIL.
2. Creating, editing, or deleting AnVIL DRS records.
3. Copying records to Syfon or Gen3.
4. Destructive delete and remote garbage collection.
5. Automatic Terra workspace-table synchronization.
6. Additional Google authentication modes.
7. Generalized support for every DRS authority or provider.
8. A broad multi-provider pointer-format redesign beyond what portable AnVIL references require.

## Definition of Done

The AnVIL/Terra reference POC is functional when:

- User A can add at least ten AnVIL DRS references in one manifest command.
- Each generated pointer commits the normalized canonical DRS URI.
- Pointers also preserve expected size and SHA256 when available.
- The Git repository contains no payload bytes, credentials, authorization headers, or signed URLs.
- Each clone configures the read-only AnVIL remote in repository-local Git configuration.
- Ordinary `git push` publishes the references without contacting AnVIL.
- `git drs push` refuses to operate on the read-only Terra remote.
- User B can clone without receiving any User A clone-local metadata, cache, or credentials.
- User B can authenticate with their own Google ADC identity and run `git drs pull` successfully.
- Full and selective hydration both work.
- An unauthorized user can clone pointers but cannot hydrate controlled-access data.
- Every download is size-verified and SHA256-verified when a checksum is available.
- Interrupted, expired-URL, and integrity-failure paths do not poison the cache or overwrite pointers with invalid content.
- Repeated pulls are idempotent and reuse verified cache entries.
- The complete two-user flow runs as a documented acceptance test in a clean environment.
