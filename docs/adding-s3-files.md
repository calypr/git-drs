# Adding Provider Objects with `git drs add-url`

`git drs add-url` prepares a Git pointer plus local DRS metadata for an object that already exists in provider storage.

Important behavior:

- `add-url` does not upload object bytes.
- Registration to drs-server happens when you run `git drs push`.
- Remote-backed inspection is performed by Syfon through stored bucket credentials and bucket scopes.
- In the current server-backed path, `add-url` supports S3 and S3-compatible storage.
- The resolved source URL (`s3://...`) is stored as the object access URL.

## Supported URL Forms

Primary support today is S3-style URLs:

- `s3://bucket/key`
- `https://bucket.s3.amazonaws.com/key`
- Path-style S3-compatible HTTPS URLs

The remote-backed inspect path is intentionally narrow in v1: S3 and S3-compatible buckets only.

## Two Add-URL Input Modes

### 1) Configured bucket object key (preferred)

If your remote org/project already has a bucket mapping, pass an object key relative to that configured bucket scope and set `--scheme`.

```bash
git drs track "data/*.bin"
git add .gitattributes

git drs add-url path/to/object.bin data/from-bucket.bin \
  --scheme s3 \
  --sha256 <64-char-sha256>
```

Notes:

- `path/to/object.bin` is resolved relative to the configured bucket prefix for the current remote org/project.
- `--scheme` is required in object-key mode so Syfon knows which provider path to inspect. Today this must be `s3`.
- The remote Syfon instance resolves bucket plus prefix from its stored bucket scopes, then performs the object HEAD using its stored bucket credential.

### 2) Raw provider URL

You can still pass a full provider URL directly.

```bash
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin \
  --sha256 <64-char-sha256>
```

## Known SHA256 (recommended when available)

If you know the authoritative SHA256, pass `--sha256`.

```bash
git drs track "data/*.bin"
git add .gitattributes

git drs add-url path/to/object.bin data/from-bucket.bin \
  --scheme s3 \
  --sha256 <64-char-sha256>

git add data/from-bucket.bin
git commit -m "add known-sha object"
git drs push
```

## Unknown SHA256

If SHA256 is unknown, omit `--sha256`.

Behavior:

1. `add-url` performs object metadata lookup (HEAD/attributes).
2. A deterministic placeholder/local OID is derived from remote object metadata.
3. A pointer file and local DRS metadata are written; the placeholder is not recorded as a content checksum.
4. The provider URL/source metadata remains the retrieval identity until content is downloaded.
5. `git drs push` performs metadata registration.

```bash
git drs track "data/*.bin"
git add .gitattributes

git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3

git add data/from-bucket.bin
git commit -m "add unknown-sha object"
git drs push
```

## Authentication and Endpoint Configuration

`add-url` no longer accepts per-command AWS flags.

For the normal remote-backed flow, `git drs add-url` does not use local `AWS_*` credentials. Syfon loads the bucket credential and endpoint configuration that were already stored through bucket management.

If your remote does not implement the internal inspect route yet, `add-url` fails with an upgrade message instead of falling back to local env-based inspection.

## Prerequisites

- File path must be tracked (via `.gitattributes`).
- Remote configuration must point to the intended org/project scope.
- The bucket credential and org/project storage scope must exist on drs-server, for example via `git drs bucket add`, then `git drs bucket add-organization` or `git drs bucket add-project --path s3://bucket/prefix`.

## Troubleshooting

### `blob attributes failed ... MovedPermanently (301)`

Usually a stored bucket credential is pointed at the wrong region or endpoint for S3-compatible storage.

- Check the Syfon bucket credential region.
- Check the Syfon bucket credential endpoint for custom S3-compatible storage.

### `no local payload available; skipping upload and keeping metadata-only registration`

Expected for add-url pointer/metadata-only flows where local payload bytes are intentionally absent.

### `file is not tracked`

Track the path pattern and re-add:

```bash
git drs track "data/*.bin"
git add .gitattributes
```
