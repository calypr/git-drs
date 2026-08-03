# Commands Reference

Current reference for the cleaned `git-drs` CLI.

> **Navigation:** [Getting Started](getting-started.md) -> **Commands Reference** -> [Troubleshooting](troubleshooting.md)

## Core Setup

### `git drs install`

Install global Git filter configuration for `git-drs`.

```bash
git drs install
```

This sets the global `filter.drs.*` entries used by Git clean/smudge/filter operations.

### `git drs init`

Initialize or repair repo-local `git-drs` wiring in the current repository.

```bash
git drs init [flags]
```

Common flags:

- `--transfers <n>`: concurrent transfers
- `--upsert`: enable upsert behavior for push/register flows
- `--multipart-threshold <mb>`: multipart threshold in MB
- `--enable-data-client-logs`: enable lower-level client logging

Use this when you want explicit initialization or to repair repo-local hooks/config. For normal onboarding, `git drs remote add ...` now bootstraps repo-local setup automatically when it is missing.

## Remote Configuration

### `git drs remote add gen3 [remote-name] <organization/project>`

Add or refresh a Gen3-backed Syfon remote.

```bash
git drs remote add gen3 [remote-name] <organization/project> --cred <credentials-file>
git drs remote add gen3 [remote-name] <organization/project> --token <bearer-token>
```

Notes:

- `remote-name` is optional; if omitted, the default remote name is used
- scope is always one positional argument: `organization/project`
- `--cred` imports a Gen3 credential file
- `--token` uses a temporary bearer token
- if repo-local `git-drs` setup is missing, this command bootstraps it
- bucket resolution is scope-driven; users normally do not provide `--bucket`

### `git drs remote add local <remote-name> <url> <organization/project>`

Add or refresh a local Syfon/DRS remote.

```bash
git drs remote add local local-dev http://localhost:8080 HTAN_INT/BForePC
git drs remote add local local-dev http://localhost:8080 HTAN_INT/BForePC --username drs-user --password drs-pass
```

Notes:

- local mode can store HTTP basic auth for helper flows
- repo-local `git-drs` setup is also bootstrapped here when missing

### `git drs remote list`

List configured `git-drs` remotes.

```bash
git drs remote list
```

### `git drs remote remove <remote-name>`

Remove a configured `git-drs` remote.

```bash
git drs remote remove <remote-name>
git drs remote rm <remote-name>
```

This removes `git-drs` remote config, not normal Git remotes.

### `git drs remote set <remote-name>`

Set the default `git-drs` remote.

```bash
git drs remote set production
```

## Tracking and Local Inventory

### `git drs track <pattern>`

Track files or globs with `git-drs` pointer behavior.

```bash
git drs track "*.bam"
git drs track "data/**"
```

Stage `.gitattributes` after changing tracked patterns.

### `git drs untrack <pattern>`

Stop tracking a pattern.

```bash
git drs untrack "*.bam"
```

### `git drs ls-files [pathspec...]`

List tracked files in the current checkout.

```bash
git drs ls-files
git drs ls-files -l
git drs ls-files --drs
git drs ls-files -I "*.bam"
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
- `--drs`: include DRS lookup details

## Hydration and Push

### `git drs pull`

Hydrate tracked pointer files already present in the current checkout.

```bash
git drs pull
git drs pull -I "*.bam"
git drs pull -I "data/**" -I "results/*.txt"
git drs pull --dry-run -I "results/**"
```

Important behavior:

- `git drs pull` does not run `git pull`
- it only hydrates tracked pointer files already present in the checkout
- include matching is against repo-relative paths

### `git drs push [remote-name]`

Run the managed `git-drs` push path.

```bash
git drs push
git drs push production
```

What it does:

- resolves local pointer/object metadata
- discovers all newly reachable LFS/DRS pointer objects in the Git object graph
- compares them with the remote `refs/git-drs/synced/*` acknowledgment ref
- registers only missing scoped metadata
- uploads only missing local payload bytes
- advances the synchronization acknowledgment only after Git and DRS work succeeds
- completes the Git push flow

Notes:

- this is the normal command for tracked data changes
- plain `git push` does not trigger `git-drs` registration or upload behavior
- synchronization is history-derived; pointers deleted from the tip remain covered while reachable from Git history
- the remote acknowledgment ref allows a later `git drs push` from another clone to recover after plain `git push`
- unreachable-object cleanup is separate from normal push and is not performed implicitly

## Provider/Object Reference Workflows

### `git drs add-url <object-url-or-key> [path]`

Create a pointer plus local DRS metadata for an object that already exists in provider storage.

```bash
git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin --sha256 <hex>
```

Notes:

- object-key mode resolves against the configured bucket scope
- explicit provider URL mode remains supported
- `--scheme` is required for object-key mode
- registration happens later on `git drs push`

### `git drs add-ref <drs-id> <path>`

Add a local pointer file for an existing DRS object.

```bash
git drs add-ref drs://example/object-id data/object.bin
```

### `git drs query <drs-id>`

Query a DRS object by ID or checksum.

```bash
git drs query drs://example/object-id
git drs query --checksum <sha256>
```

## Delete and Copy Workflows

### `git drs rm <path>...`

Remove tracked `git-drs` files from the worktree and index.

```bash
git drs rm data/sample.bam
git drs rm data/sample1.bam data/sample2.bam
```

What it does:

- validates that each path is tracked as a `git-drs` pointer-managed file
- runs `git rm` for those paths
- leaves remote DRS reconciliation to the later `git drs push`

Remote behavior on push:

- `git drs push` removes the pointer from the Git tip but does not delete the DRS record or payload
- historical DRS objects remain available while their pointers are reachable from Git history
- use the explicit `git drs delete` command when destructive DRS deletion is intended

### `git drs copy-records [source-remote] <target-remote> <organization/project>`

Copy Syfon metadata records from one configured remote to another for one scope.

```bash
git drs copy-records prod HTAN_INT/BForePC
git drs copy-records dev prod HTAN_INT/BForePC
git drs copy-records @local prod HTAN_INT/BForePC
```

Behavior:

- with one remote arg:
  - source defaults to the configured default remote
  - the arg is treated as the target remote
- with two remote args:
  - first is source
  - second is target
- copies metadata only, not object bytes
- `@local` explicitly means the current repository's local records
- the legacy `local` alias means repository-local only when no configured remote is named `local`

Merge behavior for existing target records:

- match by DID first, then by checksum where applicable
- union `controlled_access`
- union `access_methods`
- preserve existing target metadata otherwise

Use `--overwrite-existing` when the source metadata must replace existing
target metadata. This mode requires a Syfon target that supports
`PUT /index/bulk/overwrite`; it matches a checksum sibling only inside the
requested target project and preserves that target record's DID.

## Bucket Mapping Commands

These are typically steward/admin setup commands, not normal day-to-day end-user commands.

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

Map a project to a bucket path.

```bash
git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/htan-int/bforepc
```

## Version

### `git drs version`

Display version information.

```bash
git drs version
```

## Removed Legacy Commands

These commands are gone from the cleaned CLI:

- `git drs fetch`
- `git drs list`
- `git drs upload`
- `git drs download`

If older notes mention them, treat those references as stale.
