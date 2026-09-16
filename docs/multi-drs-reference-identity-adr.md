# ADR 0005: Preserve source DRS identity for reference-first workflows across mixed DRS servers

## Status

Proposed

## Context

`git-drs` supports two different classes of object tracking:

1. **Normal local-file tracking**
   - The user tracks local bytes.
   - The clean filter calculates the content SHA256.
   - The Git pointer uses `oid sha256:<content-sha256>`.
   - Downstream hydration can use the default configured DRS remote to look up DRS records by SHA256.

2. **Reference-first tracking** through `add-ref` and `add-url`
   - The user references object content that already exists somewhere else.
   - The repository may not have local payload bytes at reference creation time.
   - The referenced object may live behind a DRS server or provider/storage URL that is not the default DRS server.
   - Not every DRS server used by a repository can be assumed to support SHA256 lookup.

Today several implementation paths still rely on a SHA256-shaped local object identity. This works for local-file tracking because content bytes are available and the default remote is expected to resolve SHA256 checksums. It is not sufficient for `add-ref` and `add-url` when the source server cannot resolve by SHA256 or when the source DRS URI is the only durable retrieval identity.

The important distinction is:

- **content SHA256** identifies bytes when known;
- **local pointer/cache OID** gives `git-drs` a compact cache key and Git LFS-compatible pointer shape;
- **source DRS URI** identifies the remote DRS record and may be the only reliable way to retrieve bytes from a non-default or non-checksum-indexed DRS server.

A downstream user who clones the repository must be able to hydrate reference-first objects without relying on the default DRS server's checksum lookup when the original source server does not support it.

## Decision

Adopt a split identity model for all reference-first workflows:

1. **Normal local-file tracking remains content-SHA256 driven.**
   - The clean filter calculates the real content SHA256 from local bytes.
   - Pointers for normal tracked files continue to use `oid sha256:<content-sha256>`.
   - The default `remote` DRS server is assumed to support SHA256 lookup for these objects.
   - Downstream users hydrate these files by checksum lookup against the default remote.

2. **`add-ref` must preserve the original DRS URI as canonical source identity.**
   - `git drs add-ref <drs-uri> <path>` must store the input DRS URI as first-class metadata.
   - If the source DRS object provides a real SHA256, store it as content metadata.
   - If the source DRS object does not provide SHA256, do not fabricate a content checksum.
   - Use a SHA256-shaped local/cache OID only as local identity when needed.
   - Downstream hydration must resolve by the stored DRS URI when checksum lookup is unavailable or not appropriate.

3. **`add-url` must preserve a canonical retrievable source reference.**
   - If the input is a DRS URI, store that DRS URI as canonical source identity.
   - If the input is a provider URL such as `s3://...`, store that provider URL as source access metadata and preserve enough remote/provider context to retrieve it later.
   - If a real SHA256 is supplied through `--sha256`, store it as content metadata.
   - If SHA256 is unknown, derive only a local placeholder/cache OID; mark it as non-content-derived.
   - Downstream hydration must use the stored source reference rather than assuming checksum lookup will work.

4. **Do not use DRS-URI-derived SHA256 values as content checksums.**
   - A hash of `drs://...` is a local/cache key, not a byte checksum.
   - Download validation must compare against a real content SHA256 only when one is known.
   - If only DRS URI identity is known, validation should use size and DRS/provider metadata where available, and should clearly report that content SHA256 validation was unavailable.

5. **Pointer/cache compatibility remains SHA256-shaped internally.**
   - Existing fanout paths and many pointer workflows expect 64-hex OIDs.
   - Reference-first objects may therefore use a derived local OID such as:

```text
derived_oid = sha256("git-drs-source-ref:v1\nsource_uri=<normalized-source-uri>\nremote=<remote-name-or-type>\n")
```

   - This derived OID is only a local identifier.
   - It must be recorded with metadata that states its origin, for example `oid_kind = "derived-source-uri"`.

## Required metadata model

Reference-first metadata must make the following fields explicit:

```text
local_oid          SHA256-shaped local pointer/cache identity
local_oid_kind     content-sha256 | derived-source-uri | placeholder
source_uri         original drs:// URI or provider URL
source_type        drs | s3 | gs | https | other
source_remote      configured remote name, remote type, or resolver profile
content_sha256     real payload SHA256 when known
size               object size when known
access_methods     provider or DRS access metadata when available
```

The pointer may remain Git LFS-shaped for compatibility:

```text
version https://git-lfs.github.com/spec/v1
oid sha256:<local_oid>
size <size>
```

But the sidecar/local metadata must be committed or otherwise made available to downstream users when it is required for hydration. A SHA256-shaped pointer alone is insufficient for non-checksum DRS servers unless `<local_oid>` is a real content SHA256 and a configured DRS/index can resolve it.

A git-drs-specific pointer that embeds the DRS URI remains valid as an alternate representation:

```text
version https://calypr.github.io/spec/v1
oid drs://example/object
size <size>
```

If this representation is used, the hydration path must preserve the OID type and resolve by DRS URI, not by checksum.

## Hydration behavior

Hydration should choose the retrieval strategy from metadata, in this order:

1. **Source DRS URI path**
   - If metadata contains `source_type = drs` and `source_uri`, resolve the DRS object through `source_remote` or the recorded resolver profile.
   - Request an access URL from that source DRS server.
   - Download to the local cache path for `local_oid`.
   - Verify real SHA256 only if `content_sha256` is known.

2. **Provider URL path**
   - If metadata contains a provider URL and credentials/resolver context, retrieve through that provider path.
   - Verify real SHA256 only if `content_sha256` is known.

3. **Checksum lookup path**
   - If `local_oid_kind = content-sha256` or `content_sha256` is known, lookup by checksum on the configured remote.
   - This is the normal path for local-file tracking and remains supported for DRS servers that provide checksum lookup.

4. **Failure path**
   - If the only available identity is a derived/placeholder OID and there is no source URI/provider metadata, hydration must fail with an actionable error explaining that the repository lacks enough source identity to retrieve the object.

## `add-ref` implications

`add-ref` should no longer reduce a DRS reference to only a SHA256 pointer.

Required behavior:

1. Resolve the input DRS URI against the selected remote/resolver.
2. Capture returned size, checksums, access methods, and DRS object ID when available.
3. Persist the original input DRS URI as `source_uri`.
4. Persist the selected remote or resolver as `source_remote`.
5. Use real content SHA256 as `content_sha256` only if the DRS object reports it.
6. Use either:
   - `local_oid = content_sha256` when a real SHA256 is known and policy permits checksum identity; or
   - `local_oid = sha256(namespaced normalized source_uri + remote context)` when SHA256 is missing or when the source server cannot support checksum lookup.
7. Ensure downstream hydration resolves by `source_uri` when `source_remote` does not support SHA256 lookup.

## `add-url` implications

`add-url` should treat its input URL as source identity, not merely as a seed for a placeholder OID.

Required behavior:

1. Inspect the source object for size and metadata where possible.
2. Preserve the input object URL as `source_uri` or provider source metadata.
3. Preserve the selected remote/provider resolver context.
4. Use `--sha256` as real `content_sha256` only when supplied by the user or verified from trusted provider metadata.
5. If SHA256 is unknown, derive a local OID from namespaced source metadata and mark it as non-content-derived.
6. Ensure downstream hydration retrieves from the stored source URI/provider metadata rather than assuming checksum lookup.

## Consequences

- Normal local-file tracking remains compatible with existing SHA256 lookup behavior.
- `add-ref` and `add-url` become safe for multi-DRS repositories where some servers cannot look up by SHA256.
- Downstream users can hydrate reference-first objects because the original source identity is preserved.
- Cache paths remain compact and SHA256-shaped without confusing derived local IDs for payload checksums.
- Download verification must become metadata-aware: real SHA256 validation is required when known, but a derived local OID must never be treated as a byte checksum.
- Repository metadata must become part of the clone/hydration contract for reference-first objects.

## Non-goals

- Do not require every DRS server to implement SHA256 lookup.
- Do not require downloading payload bytes during `add-ref` or `add-url` solely to compute SHA256.
- Do not treat hashes of DRS URIs or provider URLs as real content checksums.
- Do not remove the existing SHA256-based workflow for normal local-file tracking.
