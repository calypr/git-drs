# Commands Reference

Complete reference for the `git-drs` CLI as used on the `fix/cli` line.

Git DRS owns Git/DRS orchestration and local metadata. Provider access, signed URL behavior, and cloud inspection are handled through Syfon and client code behind these commands.

> **Navigation:** [Getting Started](getting-started.md) -> **Commands Reference** -> [Troubleshooting](troubleshooting.md)

## Command Model

`git-drs` is intentionally smaller now.

- Removed legacy commands:
  - `git drs fetch`
  - `git drs list`
  - `git drs upload`
  - `git drs download`
- `git drs pull` now mirrors `git lfs pull` semantics:
  - it hydrates tracked pointer files in the current checkout
  - it does not run `git pull`
- `git drs ls-files` is the `git lfs ls-files` analog:
  - local-first inventory
  - optional DRS registration checks
- `git drs remote add gen3` now takes scope as a positional `organization/project`

## Core Setup

### `git drs install`

Install global Git filter configuration for `git-drs`.

```bash
git drs install
```

This sets the global `filter.drs.*` entries used by Git clean/smudge/filter operations.

### `git drs init`

Initialize `git-drs` in the current repository.

```bash
git drs init [flags]
```

Common flags:

- `--transfers <n>`: concurrent transfers
- `--upsert`: enable upsert behavior for push/register flows
- `--multipart-threshold <mb>`: multipart threshold in MB
- `--enable-data-client-logs`: enable lower-level client logging

Use this when you want to initialize the repo explicitly, or to repair repo-local hooks/config.

For normal onboarding, `git drs remote add ...` now auto-initializes the repository if that setup is missing.

## Remote Configuration

### `git drs remote add gen3 [remote-name] <organization/project>`

Add or refresh a Gen3-backed Syfon remote.

```bash
git drs remote add gen3 [remote-name] <organization/project> \
    --cred <credentials-file>
```

**Options:**

- `--cred <file>`: Path to credentials JSON file (required)
- `--token <token>`: Token for temporary access (alternative to --cred)
- `<organization/project>`: Required scope argument, for example `HTAN_INT/BForePC`

**Examples:**

```bash
# Add production remote
git drs remote add gen3 production my-program/my-project \
    --cred /path/to/credentials.json

# Add staging remote
git drs remote add gen3 staging staging-program/staging-project \
    --cred /path/to/staging-credentials.json
```

Notes:

- `remote-name` is optional; if omitted, the default remote name is used.
- scope is always one positional argument: `organization/project`
- `--cred` imports a Gen3 credential file
- `--token` uses a temporary bearer token
- if the repo has not been initialized yet, this command bootstraps the local `git-drs` hooks/config first
- bucket resolution is scope-driven; users do not need to provide `--bucket`
- endpoint resolution comes from the credential/token path; users do not need to provide `--url`

Prerequisite:

- the target `organization/project` must already be mapped to a bucket on the server
- if no local repo mapping exists, `git-drs` can resolve the visible bucket from the server

### `git drs remote list`

List configured DRS remotes.

```bash
git drs remote list
```

### `git drs remote remove <remote-name>`

Remove a configured DRS remote.

```bash
git drs remote remove <remote-name>
git drs remote rm <remote-name>
```

Notes:

- this removes `git-drs` remote config, not normal Git remotes
- `git remote remove <name>` does not manage `git-drs` remote config
- if the removed remote was the default and other `git-drs` remotes remain, one remaining remote becomes the new default
- if the removed remote was the last one, `git-drs` clears the default remote

### `git drs add-url <object-url-or-key> [path]`

Prepare a pointer plus local DRS metadata for an object that already exists in provider storage.

**Usage:**

```bash
# Preferred: object key resolved against configured bucket scope
git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3

# Compatibility: explicit provider URL
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin
```

**Options:**

- `--scheme <scheme>`: Required for object-key mode because local bucket mappings persist bucket/prefix, not provider scheme
- `--sha256 <hex>`: Expected SHA256 checksum when known

**What it does:**

- Resolves the effective org/project bucket scope for the current remote
- Inspects the provider object through client-owned cloud code
- Writes a Git LFS pointer into the worktree
- Stores local DRS metadata for later registration during `git drs push`

### `git drs push [remote-name]`

Push local DRS objects to server. Uploads new files and registers metadata.

**Usage:**

```bash
# Push to default remote
git drs push

# Push to specific remote
git drs push staging
git drs push production
```

**What it does:**

- Checks local `.git/drs/lfs/objects/` for DRS metadata
- For each object, uploads file to bucket if file exists locally
- If file doesn't exist locally (metadata only), registers metadata without upload
- Reconciles committed tracked-file deletions against the pushed Git ref delta
- This enables cross-remote promotion workflows

**Cross-Remote Promotion:**

Transfer DRS records from one remote to another (eg staging to production) without re-uploading files:

```bash
# Fetch metadata from staging
git drs fetch staging

# Push metadata to production (no file upload since files don't exist locally)
git drs push production
```

This is useful when files are already in the production bucket with matching SHA256 hashes. It can also be used to reupload files given that the files are pulled to the repo first.

**Note:** `fetch` and `push` are commonly used together. `fetch` pulls metadata from one remote, `push` registers it to another.

### `git drs rm <path>...`

Remove tracked DRS/LFS files from the worktree and index.

**Usage:**

```bash
git drs rm data/sample.bam
git drs rm data/sample1.bam data/sample2.bam
```

**What it does:**

- Validates that each path is tracked as a Git LFS / git-drs file
- Runs `git rm` for those paths
- Does not mutate remote DRS state immediately

**Remote behavior on push:**

When the deletion is committed and pushed:

- `git drs push` and the managed `pre-push` hook derive deleted pointers from the pushed Git commit delta
- if the scoped record has exactly one `controlled_access` entry, the whole DRS record is deleted
- if the scoped record has multiple `controlled_access` entries, only the current `organization/project` resource is removed
- underlying object bytes are not deleted by default

### `git drs copy-records [source-remote] <target-remote> <organization/project>`

Copy Syfon records for one `organization/project` scope from one configured remote to another.

**Usage:**

```bash
git drs copy-records \
  <source-remote> \
  <target-remote> \
  <organization/project>
```

Or, to copy from the configured default remote:

```bash
git drs copy-records \
  <target-remote> \
  <organization/project>
```

**Options:**

- `<source-remote>`: Source remote. Optional. Defaults to the configured default remote.
- `<target-remote>`: Target remote. Required.
- `<organization/project>`: Required scope argument, for example `HTAN_INT/BForePC`.
- `--batch-size <n>`: Source page size and target bulk write size. Default: `250`.

**What it does:**

- Reads all source records for the requested `organization/project` using Syfon's internal bulk/list APIs
- Looks up matching DIDs on the target in batches
- Creates records that do not already exist on the target
- For existing DIDs, preserves the target record and only merges:
  - `controlled_access`
  - `access_methods`

**Merge semantics for existing target records:**

- Existing target metadata is preserved
- `controlled_access` becomes the union of source and target values
- `access_methods` becomes the union of source and target values
- Records with no effective change are skipped

**Example:**

```bash
git drs copy-records \
  dev \
  prod \
  HTAN_INT/BForePC
```

**When to use it:**

- Promote DRS metadata between Syfon instances
- Backfill `controlled_access` and `access_methods` onto an existing target instance
- Copy project-scoped records without re-uploading object bytes

### `git drs query`

Query a DRS object by its DRS ID or SHA256 checksum.

**Usage:**

```bash
# Query by DRS ID (default behavior)
git drs query <drs-id>

# Query by SHA256 checksum
git drs query --checksum <sha256>
```

## Bucket Mapping

These commands are typically steward/admin setup, not day-to-day end-user commands.

### `git drs bucket add`

Declare bucket credentials for a remote.

### `git drs bucket add-organization`

Map an organization to a bucket path.

```bash
git drs bucket add-organization production \
  --organization HTAN_INT \
  --path s3://cbds/htan-int
```

### `git drs bucket add-project`

```bash
# Known SHA path
git drs add-url s3://bucket/path/file.bin data/file.bin --sha256 <sha256>

# Unknown SHA path
git drs add-url s3://bucket/path/file.bin data/file.bin
```

**Options:**

- `--sha256 <hash>`: Optional SHA256 hash of the source object.  
  If omitted, add-url uses an ETag+source-derived placeholder OID and registers metadata without a local payload blob.

**Notes:**

- `add-url` no longer accepts per-command AWS credential flags.
- S3 connection hints are resolved from environment/runtime config when needed (for example `AWS_REGION`, `AWS_ENDPOINT_URL`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`).
- Registration happens on `git drs push`, not at `add-url` time.

### `git drs version`

Display Git DRS version information.

```bash
git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/htan-int/bforepc
```

## File Tracking and Hydration

### `git drs track`

Track files or patterns with Git-compatible pointer behavior.

```bash
git drs track "*.bam"
git drs track "data/**"
```

Stage `.gitattributes` after changing tracked patterns.

### `git drs untrack`

Stop tracking patterns.

```bash
git drs untrack "*.bam"
```

### `git drs ls-files [pathspec...]`

List tracked LFS-style files in the current checkout.

```bash
git drs ls-files
git drs ls-files data/**
git drs ls-files -I "*.bam"
git drs ls-files --drs
git drs ls-files -l --drs
git drs ls-files -n results/**
```

Important behavior:

- default mode is local-first and cheap
- `*` means localized/hydrated in the worktree
- `-` means the worktree still contains a pointer
- `--drs` adds DRS registration checks

Common flags:

- `-I, --include <pattern>`: include filter; may be repeated
- `-l, --long`: long output
- `-n, --name-only`: path-only output
- `--json`: structured output
- `--drs`: check DRS registration status

### `git drs pull`

Hydrate tracked pointer files in the current checkout.

```bash
git drs pull
git drs pull -I "*.bam"
git drs pull -I "data/**" -I "results/*.txt"
git drs pull --dry-run -I "results/**"
```

Important behavior:

- `git drs pull` does not run `git pull`
- it only hydrates tracked pointer files already present in the current checkout
- include matching is against repo-relative paths

Common flags:

- `-I, --include <pattern>`: include filter; may be repeated
- `--dry-run`: show what would be hydrated without downloading

## Object Registration and Push

### `git drs push [remote-name]`

Register and upload tracked objects, then rely on normal Git push for refs.

```bash
git drs push
git drs push production
```

What it does:

- resolves local pointer/object metadata
- uploads local bytes when needed
- registers object metadata with the target Syfon instance
- reconciles committed deletes derived from the pushed ref delta

Notes:

- delete reconciliation is Git-history-derived; there is no local delete-intent sidecar state
- `git drs push` uses the current branch upstream as the delete diff base when one exists
- plain `git push` uses the managed `pre-push` hook, which receives authoritative old/new SHAs from Git

### `git drs add-url <object-url-or-key> [path]`

Create a pointer and local metadata for an object that already exists in provider storage.

```bash
git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin --sha256 <hex>
```

Notes:

- object-key mode resolves against the configured bucket scope
- explicit provider URL mode remains supported
- `--scheme` is required for object-key mode

### `git drs add-ref <drs-id> <path>`

Add a local pointer file for an existing DRS object.

```bash
git drs add-ref drs://example/object-id data/object.bin
```

### `git drs query <drs-id>`

Query a DRS object by ID.

```bash
git drs query drs://example/object-id
```

## Metadata Copy

### `git drs copy-records [source-remote] <target-remote> <organization/project>`

Copy Syfon metadata records from one remote to another for a single project scope.

```bash
git drs copy-records prod HTAN_INT/BForePC
git drs copy-records dev prod HTAN_INT/BForePC
```

Behavior:

- with one remote arg:
  - source defaults to the configured default remote
  - arg is treated as the target remote
- with two remote args:
  - first is source
  - second is target
- copies metadata only, not object bytes

Merge behavior for existing target records:

- match by DID
- union `controlled_access`
- union `access_methods`
- preserve existing target metadata otherwise

## Removed Legacy Commands

These commands are gone from the cleaned CLI:

- `git drs fetch`
- `git drs list`
- `git drs upload`
- `git drs download`

If older docs or notes mention them, treat those references as stale.
