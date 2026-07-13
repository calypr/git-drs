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

## Practical review guidance

When reviewing pointer changes:

- `oid sha256:<64-hex>` means either a real content SHA256 or a SHA256-shaped local OID; check source metadata when the object came from `add-url` without `--sha256`.
- `oid drs://...` means the pointer itself preserves the source DRS URI and hydration should resolve by DRS URI, not by checksum lookup.
- A hash derived from a DRS URI or provider URL must not be used as a content checksum.
- Temporary access URLs may be created during hydration, but they should not replace the durable source identity in committed pointer state.
