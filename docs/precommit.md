---

# Developer Documentation: `.git/drs/pre-commit` Cache

## Overview

`git-drs` keeps two different kinds of local state under `.git/drs/`:

```text
.git/drs/
  lfs/objects/    authoritative local DRS metadata objects
  pre-commit/     rebuildable local cache for path/OID/url hints
```

They serve different jobs:

* `.git/drs/lfs/objects` stores the local DRS metadata records that commands such as `git drs push` and `git drs query` use directly.
* `.git/drs/pre-commit` is a non-authoritative cache used to keep local path/OID bookkeeping coherent for the workflows that still write it today: `git drs precommit` and `git drs add-url`.

Plain `git push` does not read this cache. `git drs push` also does not depend on it for metadata registration.

## Current Ownership

### `cmd/precommit`

* Runs from the repo's `pre-commit` hook.
* Reads staged Git content only.
* Updates path and OID cache entries for tracked pointer files.
* Never performs network I/O.

### `cmd/addurl`

* Writes a pointer file into the worktree.
* Writes the local DRS metadata object under `.git/drs/lfs/objects`.
* Updates the pre-commit cache so the new path/OID/object URL hint stay locally coherent.

### `internal/precommit_cache`

* Owns the cache layout definition shared by commands.
* Provides the cache root/paths discovery logic plus the shared JSON types.
* Does not own general cache mutation logic.

## Cache Properties

The cache is:

* local to one checkout
* never committed to Git
* safe to delete and rebuild
* non-authoritative

The authoritative local metadata store remains `.git/drs/lfs/objects`.

## On-Disk Layout

```text
.git/drs/pre-commit/v1/
  paths/
    <encoded-path>.json
  oids/
    <oid-hash>.json
  tombstones/     optional, precommit-owned
  state.json      reserved
```

### Path Entry

`paths/<encoded-path>.json`

```json
{
  "path": "data/foo.bam",
  "lfs_oid": "sha256:abc123...",
  "updated_at": "2026-02-01T12:34:56Z"
}
```

### OID Entry Written By `add-url`

`oids/<oid-hash>.json`

```json
{
  "lfs_oid": "sha256:abc123...",
  "paths": [
    "data/foo.bam"
  ],
  "external_url": "s3://bucket/key",
  "updated_at": "2026-02-01T12:34:56Z",
  "content_changed": false
}
```

### Legacy Read Compatibility

Older cache entries may still contain:

```json
{
  "s3_url": "s3://bucket/key"
}
```

`internal/precommit_cache` still reads that legacy field, but current writers now normalize back to `external_url` on rewrite.

## Rebuild Story

If `.git/drs/pre-commit` is deleted:

* `git drs precommit` will repopulate entries from staged pointer changes.
* `git drs add-url` will recreate entries for files it writes.
* `.git/drs/lfs/objects` is unaffected.

This means cache cleanup is safe as long as commands that own the cache keep their current write paths.
