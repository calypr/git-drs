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

Run this once per repository.

## Remote Configuration

### `git drs remote add gen3 [remote-name] <organization/project>`

Add or refresh a Gen3-backed Syfon remote.

```bash
git drs remote add gen3 [remote-name] <organization/project> [--cred <file> | --token <token>]
```

Examples:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/credentials.json
git drs remote add gen3 staging HTAN_INT/BForePC --token "$GEN3_TOKEN"
```

Notes:

- `remote-name` is optional; if omitted, the default remote name is used.
- scope is always one positional argument: `organization/project`
- `--cred` imports a Gen3 credential file
- `--token` uses a temporary bearer token
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

### `git drs remote set <name>`

Set the default DRS remote.

```bash
git drs remote set production
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

Map an organization/project to a bucket path.

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
