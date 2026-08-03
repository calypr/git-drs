# ADR 0003: Use DRS URI as canonical remote identity and a derived compact OID for local pointer/cache identity

## Status
Proposed

## Context

`git-drs` no longer needs to treat the Git LFS-style `oid` as a literal content SHA256 in every workflow.

This is most obvious in `add-url` and other metadata-first flows:

- the authoritative remote object identity is the DRS object itself
- the authoritative retrieval source is the DRS metadata (`access_methods`, checksums, scoped metadata)
- local pointer files and local cache paths still need a compact identifier

Today the code still assumes the local pointer/cache identifier is shaped like a Git LFS SHA256:

- pointers are parsed only when they contain `oid sha256:<64-hex>`
- local object fanout paths assume a 64-character hex identifier
- some code still conflates "pointer OID" with "content SHA256"

At the same time, raw DRS URIs are not a good direct substitute for the pointer `oid` slot:

- they are long and punctuation-heavy
- they are awkward as local cache keys and filesystem fanout paths
- they couple local identity directly to remote URI syntax
- they reduce cache/dedupe reuse if the same bytes exist under multiple DRS records

So we need to separate two concepts that are currently blurred together:

1. **Canonical remote identity**
   - the DRS URI / DRS object ID that identifies the remote record
2. **Local pointer/cache identity**
   - a compact, filesystem-safe identifier used in pointer files, cache keys, and worktree inventory

## Decision

Adopt this identity model:

1. **DRS URI is the canonical remote identity**
   - it identifies the remote record
   - it is the primary stable reference for metadata-first workflows
2. **A derived compact OID is the local pointer/cache identity**
   - it is derived deterministically from the canonical remote identity
   - it is used in pointer files, local fanout paths, and cache indexing
3. **Do not use the raw DRS URI as the literal pointer OID**
   - the pointer/cache slot remains compact and derived
4. **Do not require the derived compact OID to equal the content SHA256**
   - content SHA256 remains useful metadata when known
   - but it is not required to be the primary local identity in metadata-first flows

## Recommended derived OID format

The derived local OID should be:

- deterministic
- compact
- filesystem-safe
- independent of transient access URLs

Recommended derivation:

```text
derived_oid = sha256("git-drs-object:v1\ndrs_uri=<normalized-drs-uri>\n")
```

This gives:

- a stable 64-hex identifier
- compatibility with existing local fanout assumptions
- independence from raw URL punctuation/length
- a clean boundary between remote identity and local cache identity

## Why not use the raw DRS URI as the pointer OID?

Because the pointer OID slot behaves like a local key, not just a human-readable identifier.

Using raw DRS URI directly would:

- make pointer parsing and cache fanout messy
- force local storage assumptions to depend on URI syntax
- complicate path encoding, diagnostics, and inventory code
- make future URI normalization/versioning harder

The DRS URI should be stored as first-class metadata, not jammed directly into the pointer key slot.

## Before

The current model is effectively:

- pointer OID is assumed to be `sha256:<64hex>`
- local cache fanout is based on that value
- some workflows use placeholders, but still pretend the OID slot is "the SHA256"

This causes confusion:

- "OID" and "content checksum" are treated as the same thing even when they are not
- metadata-first objects need placeholder semantics
- design discussions keep returning to "can we use md5, etag, or path instead?"

## After

The identity model becomes explicit:

- **DRS URI** = canonical remote identity
- **derived OID** = compact local pointer/cache identity
- **checksums** = content metadata, not always the primary local key

In practical terms:

- pointer files keep a compact OID
- local fanout code keeps a compact OID
- remote record matching and canonical reference logic prefer DRS URI
- content SHA256 remains available when known, but no longer defines the whole identity model

## Implementation guidance

### Phase 1: Introduce explicit identity types

Add an internal identity structure such as:

```text
ObjectIdentity
  - DRSURI
  - OID
  - Checksums
```

This is the critical first step. It prevents more code from assuming:

```text
OID == content SHA256
```

### Phase 2: Keep pointer/cache format compact

Retain the existing compact local OID shape for compatibility:

- `oid sha256:<64hex>` in pointer files
- existing local fanout layout

But redefine the meaning:

- in metadata-first flows, the 64-hex value is the derived local identity
- not necessarily the file content SHA256

This avoids a broad pointer-format migration up front.

### Phase 3: Store DRS URI as authoritative metadata

Wherever local DRS metadata is written or read:

- persist the DRS URI explicitly
- treat it as the canonical remote reference

This applies especially to:

- `add-url`
- `add-ref`
- local DRS object files
- copy/query/register reconciliation logic

### Phase 4: Remove code that equates OID and checksum

Audit and reduce assumptions in:

- pointer parsing/writing helpers
- inventory
- push/pull resolution
- registration/update logic
- cache helpers

The main rule:

- if code wants a checksum, ask for a checksum
- if code wants a pointer/cache key, ask for the derived OID
- if code wants remote identity, ask for the DRS URI

## Consequences

### Positive

- clarifies identity semantics
- makes metadata-first workflows more coherent
- avoids raw DRS URI leakage into local cache/path mechanics
- preserves compact local storage behavior
- reduces pressure to misuse ETag/MD5/path directly as pointer OIDs

### Negative

- requires a modest identity refactor
- some current code still assumes pointer OID is content SHA256
- compatibility language around `oid sha256:` becomes semantically looser until or unless the external pointer format is generalized later

## Explicit non-goals

This ADR does **not** require:

- raw DRS URI to become the literal pointer OID
- multi-algorithm pointer syntax (`oid md5:...`, `oid etag:...`, etc.) immediately
- full removal of the current compact SHA256-shaped local OID format

Those may be considered later, but they are not required to fix the architectural confusion now.

## Summary

The correct split is:

- **DRS URI** for canonical remote identity
- **derived compact OID** for local pointer/cache identity

That gives `git-drs` a cleaner architecture without forcing local storage and pointer behavior to inherit all the messiness of raw remote identifiers.
