# Pointer Files and Reference State

`git-drs` keeps large payload bytes out of Git history by committing small pointer files. A pointer records enough identity to let `git-drs` find or rebuild the payload later, while the payload itself lives in local cache, provider storage, or behind a DRS server.

This page documents the pointer shapes used by reference-first workflows and how state changes as commands create references, download content, and obtain temporary access URLs (the resolved "real" download URLs used for the transfer).

## Pointer file shapes

### Content SHA256 pointer

When the real payload SHA256 is known, `git-drs` uses a Git LFS-compatible pointer:

```text
version https://git-lfs.github.com/spec/v1
oid sha256:<content-sha256-or-local-oid>
size <bytes>
```

For normal tracked-file workflows, `<content-sha256-or-local-oid>` is the real content SHA256. For metadata-first workflows, it may be a SHA256-shaped local/cache OID only when separate metadata preserves the retrievable source identity.

### DRS URI pointer

When `add-ref` resolves a DRS object that does not provide a real SHA256, `git-drs` preserves the source identity directly in a git-drs pointer:

```text
version https://calypr.github.io/spec/v1
oid drs://<drs-host-or-resolver>/<object-id>
size <bytes>
```

The DRS URI is the retrieval identity. It is not a content checksum. Cache paths can still use a deterministic SHA256-shaped key derived from the DRS URI, but validation must not compare downloaded bytes against that derived key as if it were a content SHA256.

## State model

The same path can move through these states:

| State | Worktree file | Local cache | Source metadata | Retrieval behavior |
|---|---|---|---|---|
| Pointer only | Pointer text is present at the tracked path | Payload may be absent | DRS URI, provider URL, or checksum metadata identifies the source | `git drs pull` can hydrate the path later |
| Cached payload | Pointer text remains in Git/index history, and payload bytes exist in `.git/lfs/objects/...` | Payload bytes are present | Same source metadata remains available | checkout/smudge can replace pointer text with payload bytes |
| Hydrated worktree | The tracked path contains payload bytes | Payload bytes are present | Source metadata remains the retrieval contract | Future clean/filter operations can write the pointer back to Git |

A DRS/provider access URL is usually not stable source identity. It is a temporary, resolved "real" download URL created from the stable DRS URI or provider reference when content is downloaded.

## Command-created state changes

### `git drs add-ref <drs-uri> <path>`

`add-ref` creates a reference to an existing DRS object without downloading payload bytes solely to compute a checksum.

1. The command resolves the input DRS URI through the selected remote/resolver.
2. If the DRS object reports a real SHA256, `git-drs` writes a SHA256 pointer whose OID is that content checksum.
3. If the DRS object does not report a real SHA256, `git-drs` writes a DRS URI pointer whose OID preserves the original `drs://...` source reference.
4. The fetched DRS metadata is stored locally under a deterministic local/cache OID.
5. The worktree path is in the **pointer only** state until content is hydrated.

Example pointer when SHA256 is unavailable:

```text
version https://calypr.github.io/spec/v1
oid drs://example.org/object-1
size 123456
```

### `git drs add-url <object-url-or-key> [path]`

`add-url` creates a reference to an object that already exists in provider storage.

1. The command inspects the provider object through the configured remote.
2. The input provider URL or resolved provider URL is preserved as source access metadata.
3. If `--sha256 <hex>` is supplied or a trusted SHA256 is discovered, that value is stored as content metadata and used as the pointer/cache OID.
4. If SHA256 is unknown, `git-drs` derives a placeholder/local OID from source metadata and writes a pointer with that SHA256-shaped local OID; it does **not** record the placeholder as a content checksum.
5. The worktree path is in the **pointer only** state until content is hydrated.

Example pointer when SHA256 is unknown:

```text
version https://git-lfs.github.com/spec/v1
oid sha256:<derived-local-oid>
size 123456
```

In that case, `<derived-local-oid>` is a local/cache identifier, not a payload checksum.

### `git drs pull`

`pull` changes pointer-only paths into hydrated content when payload bytes are missing locally.

1. The command scans tracked pointer files in the current checkout.
2. For a content SHA256 pointer, it can look up a scoped DRS record by checksum and request a temporary access URL from that DRS record.
3. For a DRS URI pointer, it resolves the stored `drs://...` source URI, requests a temporary access URL for that DRS object, and downloads through the resolved URL.
4. For provider-backed references, it uses the stored provider/source metadata to retrieve content when available.
5. Downloaded bytes are written to the local cache path.
6. If a real content SHA256 is known, the downloaded bytes are validated against it. If only a DRS URI or derived local OID is known, validation is limited to size and available DRS/provider metadata.
7. The worktree file is checked out from the cache, moving it to the **hydrated worktree** state.

The temporary access URL (the resolved "real" URL) created during this process is a download mechanism, not the durable identity committed in Git. The durable identity remains the pointer plus source metadata: content SHA256 when known, DRS URI for DRS references, or provider URL/source metadata for provider references.

## Primary remote DRS server state

The **primary remote DRS server** is the configured `drs.default-remote` or the remote passed to a command. For reference-first flows, distinguish three places where state can exist:

- **Git state:** committed pointer text at the repository path.
- **Local git-drs state:** sidecar DRS object JSON under the repo's local DRS metadata directory, plus optional local LFS cache bytes.
- **Primary remote DRS server state:** Syfon/Gen3 DRS records, scoped to the configured organization/project, and any remote storage object those records point at.

`add-ref` and `add-url` are intentionally metadata-first commands. They create Git and local git-drs state first; they do not necessarily create or mutate the primary remote DRS server until a later `git drs push` or an explicit server-side copy/register operation.

### Sequence: pointer and DRS server state for `add-ref` / `add-url`

The following sequence diagram shows the expected pointer-file changes, local
metadata/cache changes, and primary remote DRS server changes for the
reference-first `add-ref`/`add-url` flows. If a caller says `add-reg` in this
context, treat it as the registration side of the same reference-first flow:
the server-side record is created or refined only when metadata is pushed or
explicitly registered, not when the local pointer is first written.

```mermaid
sequenceDiagram
    autonumber
    actor User
    participant Git as Git worktree/index
    participant Local as Local git-drs metadata/cache
    participant Remote as Primary remote DRS server
    participant Source as Source DRS/provider object

    rect rgb(238, 248, 255)
        note over User,Source: add-ref: reference an existing DRS URI
        User->>Remote: git drs add-ref drs://source/object data/file
        Remote->>Source: Resolve DRS URI / fetch DRS object metadata
        Source-->>Remote: DRS object metadata, size, access methods, optional sha256
        Remote-->>User: Resolved DRS object metadata
        alt Resolved object includes real sha256
            User->>Git: Write pointer oid sha256:<real-content-sha256>
            User->>Local: Store DRS metadata keyed by real sha256
        else Resolved object does not include real sha256
            User->>Git: Write git-drs pointer oid drs://source/object
            User->>Local: Store DRS metadata keyed by derived DRS/local cache key
        end
        note over Remote: No new scoped DRS record is required merely because add-ref wrote a local pointer.
    end

    rect rgb(248, 255, 238)
        note over User,Source: add-url: reference an existing provider object
        User->>Remote: git drs add-url s3://bucket/key data/file [--sha256]
        Remote->>Source: Inspect provider object using remote bucket credentials/scope
        Source-->>Remote: Provider URL, size, ETag/metadata, optional trusted sha256
        Remote-->>User: Inspected object metadata
        alt Real sha256 supplied or trusted
            User->>Git: Write pointer oid sha256:<real-content-sha256>
            User->>Local: Store local DRS metadata with checksum sha256=<real-content-sha256>
        else Real sha256 unknown
            User->>Local: Calculate placeholder/local OID from provider metadata
            User->>Git: Write pointer oid sha256:<placeholder-local-oid>
            User->>Local: Store provider access URL and size without checksum=<placeholder-local-oid>
        end
        note over Remote: Inspection does not itself create the durable DRS record for this repo scope.
    end

    rect rgb(255, 248, 238)
        note over User,Remote: Push/register metadata with primary remote
        User->>Remote: git drs push / register metadata for reachable pointers
        alt Pointer/local metadata has real sha256
            Remote->>Remote: Upsert scoped DRS record with sha256, size, and access method
        else Only DRS URI/provider source identity is known
            Remote->>Remote: Upsert metadata preserving source URI/provider access, size, and no fake sha256
        end
    end

    rect rgb(255, 238, 248)
        note over User,Source: Later hydration discovers real content sha256
        User->>Remote: git drs pull resolves DRS URI or source metadata
        Remote->>Source: Request temporary access URL / download bytes
        Source-->>Local: Store downloaded payload bytes in cache/worktree
        Local->>Local: Calculate real sha256 over downloaded bytes
        Local->>Local: Store/refine local checksum metadata sha256=<real-content-sha256>
        opt User intentionally rewrites pointer metadata
            User->>Git: Replace DRS/placeholder pointer with oid sha256:<real-content-sha256>
        end
        opt User pushes refined metadata
            User->>Remote: git drs push / register refined checksum metadata
            Remote->>Remote: Store real sha256 as checksum; keep DRS/provider retrieval identity
            Remote->>Remote: Ensure placeholder or derived local OID is not advertised as checksum
        end
    end
```

### After `git drs add-ref <drs-uri> <path>`

Expected state immediately after `add-ref` succeeds:

| Location | Expected state |
|---|---|
| Git/worktree | A pointer file exists at `<path>`. If the resolved DRS object has a real SHA256 checksum, the pointer uses `oid sha256:<content-sha256>`; otherwise it uses a git-drs DRS URI pointer that preserves the input DRS URI. |
| Local git-drs metadata | The resolved DRS object returned by the selected remote/resolver is stored locally under the local OID used for the pointer/cache lookup. If the DRS object omitted `self_uri`, the input DRS URI is retained as `self_uri`. |
| Local payload cache | No payload download is required just to create the reference. The cache may still be empty for this object. |
| Primary remote DRS server | No new record is required merely because `add-ref` ran. The primary remote must be able to resolve the source DRS URI for future hydration, but `add-ref` should be treated as creating a local reference to an existing DRS record, not as copying or registering that record into a new project scope. |

If the input DRS URI points at another DRS authority or resolver, the primary remote's durable state after `add-ref` is still unchanged unless the server itself implements and is asked to persist a proxy/copy record. The committed reference remains valid because the pointer/local metadata preserve the source DRS URI.

### After `git drs add-url <object-url-or-key> [path]`

Expected state immediately after `add-url` succeeds:

| Location | Expected state |
|---|---|
| Git/worktree | A Git LFS-shaped pointer file exists at `[path]`. With `--sha256`, its OID is the real content SHA256. Without `--sha256`, its OID is a deterministic SHA256-shaped placeholder/local OID derived from inspected provider metadata. |
| Local git-drs metadata | A local DRS object is written with the resolved provider URL as an access method. When `--sha256` is provided, the local DRS object includes a `sha256` checksum. When SHA256 is unknown, the placeholder/local OID is **not** written as a checksum. |
| Local payload cache | No payload upload or download is required. The object bytes are expected to already exist at the provider URL inspected by the remote. |
| Primary remote DRS server | No DRS record is required immediately. On a later `git drs push`, git-drs can bulk-register metadata for the pointer into the primary remote scope. For known-SHA objects, the remote record should include the real SHA256 and size. For unknown-SHA objects, the remote record must preserve the provider access URL and size, and must not claim that the placeholder/local OID is a real content checksum. |

A primary remote may already know about the provider object because of its bucket credential/scope configuration. That inspection ability is not the same thing as a durable DRS record for the repository object; durable registration is a push-time concern.

### After a DRS URI is downloaded and a real SHA256 is calculated

Downloading through a DRS URI has two separate effects:

1. It hydrates local state by resolving the stable DRS URI to a temporary access URL and writing payload bytes into the local cache/worktree.
2. It may reveal the real content SHA256 if the downloaded bytes are hashed locally.

Expected state after the real SHA256 is known:

| Location | Expected state |
|---|---|
| Git/worktree | Existing committed pointer identity should not silently change during download. A DRS URI pointer remains a DRS URI pointer until the user/tool intentionally rewrites and commits it as a content-SHA pointer. |
| Local payload cache | The downloaded bytes exist in the local cache. If a previous cache key was derived from a DRS URI or placeholder, that key remains a local retrieval/cache key, not proof of content identity. Implementations may also add an alias/cache entry keyed by the real SHA256, but should avoid losing the source DRS URI mapping. |
| Local git-drs metadata | The real SHA256 can be added as checksum metadata alongside the preserved DRS URI/provider source identity. The DRS URI/provider URL remains the durable retrieval contract unless and until a remote record with the real checksum is registered. |
| Primary remote DRS server | Merely downloading and hashing bytes locally should not mutate the primary remote. If the repository later pushes an updated metadata record, the primary remote should have a scoped DRS record whose `sha256` is the real content checksum, whose `size` matches the downloaded payload, and whose access methods still resolve to the original DRS/provider source or to a managed copied object. The remote should not retain a placeholder/local OID as a checksum once the real SHA256 is known. |

When a placeholder or DRS-derived local OID is replaced in metadata by a real SHA256, treat it as a metadata refinement, not as evidence that the original source identity was wrong. The safe final server state is: **real content SHA256 for checksum-based lookup, stable DRS URI/provider access metadata for retrieval, and no placeholder/local OID advertised as a content checksum.**

## Practical review guidance

When reviewing pointer changes:

- `oid sha256:<64-hex>` means either a real content SHA256 or a SHA256-shaped local OID; check source metadata when the object came from `add-url` without `--sha256`.
- `oid drs://...` means the pointer itself preserves the source DRS URI and hydration should resolve by DRS URI, not by checksum lookup.
- A hash derived from a DRS URI or provider URL must not be used as a content checksum.
- Temporary access URLs may be created during hydration, but they should not replace the durable source identity in committed pointer state.
