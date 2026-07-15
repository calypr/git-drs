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

### `git drs ping [remote-name]`

Show the effective `git-drs` remote configuration and verify that the remote responds.

```bash
git drs ping
git drs ping anvil
```

What it checks:

- prints the selected remote, remote type, endpoint, scope, bucket, storage prefix, and auth mode
- runs a health check against the selected remote
- for Terra DRS remotes, pings the GA4GH DRS service-info endpoint at `<endpoint>/ga4gh/drs/v1/service-info`
- for Terra/AnVIL TDR-hosted data in production, use `https://data.terra.bio` as the endpoint; Terra also uses DRSHub for DRS URI resolution, but DRSHub is a resolver service rather than the GA4GH DRS service-info host
- for scoped Syfon-style remotes, verifies that the configured organization/project and bucket are visible and readable

Example Terra configuration and ping:

```bash
git config drs.default-remote anvil
git config drs.remote.anvil.type terra
git config drs.remote.anvil.endpoint https://data.terra.bio
git config drs.remote.anvil.auth google-adc
git config drs.remote.anvil.mode read-only
git drs ping anvil
```

A successful Terra ping includes:

```text
remote: anvil (default)
type: terra
endpoint: https://data.terra.bio
health: ok
```

Troubleshooting:

- `no remote configuration found`: run `git drs remote list` and pass an existing remote name, or configure the Terra remote with the `git config` commands above.
- `terra endpoint is empty`: set `drs.remote.<name>.endpoint` to the Terra DRS base URL, for example `https://data.terra.bio` for the production Terra Data Repository DRS service.
- `terra endpoint must be an absolute URL`: include the URL scheme, for example `https://data.terra.bio` instead of `data.terra.bio`.
- `terra DRS service-info returned ...`: verify the server is up and that the base endpoint is correct. You can test the exact URL with `curl -i https://data.terra.bio/ga4gh/drs/v1/service-info`.
- network, DNS, or TLS errors: check VPN/proxy/firewall settings and retry with `GIT_CURL_VERBOSE=1 git drs ping <remote-name>` for additional HTTP diagnostics from Git-adjacent workflows.

For developers, the live Terra ping integration test is intentionally behind the `integration` build tag because it reaches the public Terra DRS service:

```bash
go test -tags=integration ./cmd/ping -run TestIntegrationPingTerraDRSServer -count=1
```

Set `GIT_DRS_TERRA_DRS_ENDPOINT` to point the test at a different Terra DRS deployment. Terra's DRSHub resolver URL follows the `https://drshub.dsde-<env>.broadinstitute.org/api/v4/drs/resolve` pattern used by `terra-notebook-utils`, but `git drs ping` needs the GA4GH DRS service base URL that exposes `/ga4gh/drs/v1/service-info`; for Terra production that service base URL is `https://data.terra.bio`.

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

For details on pointer file formats and lifecycle state, see [Pointer Files and Reference State](pointer-files.md).

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
- when `--sha256` is omitted, the pointer uses a derived local/cache OID and source URL metadata remains the retrieval identity
- registration happens later on `git drs push`

### `git drs add-ref <drs-id> <path>`

Add a local pointer file for an existing DRS object. If the source DRS object has a SHA256 checksum, the pointer can use that checksum; otherwise the pointer preserves the source `drs://...` URI directly so hydration can resolve by DRS identity instead of checksum lookup.

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
```

Behavior:

- with one remote arg:
  - source defaults to the configured default remote
  - the arg is treated as the target remote
- with two remote args:
  - first is source
  - second is target
- copies metadata only, not object bytes

Merge behavior for existing target records:

- match by DID first, then by checksum where applicable
- union `controlled_access`
- union `access_methods`
- preserve existing target metadata otherwise

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
