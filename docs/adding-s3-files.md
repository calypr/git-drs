# Adding Provider Objects with `git drs add-url`

`git drs add-url` prepares a Git LFS pointer plus local DRS metadata for an object that already exists in provider storage.

Important behavior:

- `add-url` does not upload object bytes.
- Registration to drs-server happens when you run `git drs push`.
- Direct provider inspection is client-owned behavior routed through `syfon/client/cloud`.
- The resolved source URL (`s3://...`, `gs://...`, `azblob://...`, etc.) is stored as the object access URL.

## Supported URL Forms

Primary support today is S3-style URLs:

- `s3://bucket/key`
- `https://bucket.s3.amazonaws.com/key`
- Path-style S3-compatible HTTPS URLs

The inspector also accepts other go-cloud styles (`gs://`, `azblob://`, `file://`), but the main production path in current e2e coverage is S3/Gen3 bucket-backed workflows.

## Two Add-URL Input Modes

### 1) Configured bucket object key (preferred)

If your remote org/project already has a bucket mapping, pass an object key relative to that configured bucket scope and set `--scheme`.

```bash
git lfs track "data/*.bin"
git add .gitattributes

git drs add-url path/to/object.bin data/from-bucket.bin \
  --scheme s3 \
  --sha256 <64-char-sha256>
```

Notes:

- `path/to/object.bin` is resolved relative to the configured bucket prefix for the current remote org/project.
- `--scheme` is required in object-key mode because local bucket mappings store bucket/prefix, but not provider scheme.
- Azure object-key mode is not supported yet; use a full `azblob://...` URL so account metadata stays explicit.

### 2) Raw provider URL (compatibility mode)

You can still pass a full provider URL directly.

```bash
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin \
  --sha256 <64-char-sha256>
```

## Known SHA256 (recommended when available)

If you know the authoritative SHA256, pass `--sha256`.

```bash
git lfs track "data/*.bin"
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
2. A deterministic placeholder OID is derived from ETag and source URL.
3. `git drs push` performs metadata-only registration until real payload bytes exist locally.

```bash
git lfs track "data/*.bin"
git add .gitattributes

git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3

git add data/from-bucket.bin
git commit -m "add unknown-sha object"
git drs push
```

## Authentication and Endpoint Configuration

`add-url` no longer accepts per-command AWS flags.

S3 connection hints are resolved from runtime environment/config. Common variables:

- `AWS_REGION` (or `AWS_DEFAULT_REGION`)
- `AWS_ENDPOINT_URL_S3` (or `AWS_ENDPOINT_URL`)
- `AWS_ACCESS_KEY_ID`
- `AWS_SECRET_ACCESS_KEY`

For e2e/dev harnesses, `TEST_BUCKET_*` variables are also supported by command-layer wiring.

## Prerequisites

- File path must be LFS-tracked (via `.gitattributes`).
- Remote configuration must point to the intended org/project scope.
- The bucket credential and org/project storage scope must exist on drs-server, for example via `git drs bucket add`, then `git drs bucket add-organization` or `git drs bucket add-project --path s3://bucket/prefix`.

## Troubleshooting

### `blob attributes failed ... MovedPermanently (301)`

Usually region/endpoint mismatch for S3-compatible storage.

- Set `AWS_REGION` correctly.
- Set `AWS_ENDPOINT_URL_S3` for custom endpoints (MinIO/Ceph/Gen3 object gateway).

### `no local payload available; skipping upload and keeping metadata-only registration`

Expected for add-url pointer flows where local payload bytes are intentionally absent.

### `file is not tracked by LFS`

Track the path pattern and re-add:

```bash
git lfs track "data/*.bin"
git add .gitattributes
```
