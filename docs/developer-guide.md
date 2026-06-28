# Developer Guide

This guide describes the current `git-drs` architecture after the CLI and internal cleanup passes.

## High-Level Model

`git-drs` sits between normal Git workflows and Syfon/Gen3-backed object workflows.

Use the tools at the right layer:

- use `git` for commits, branches, merges, `git pull`, and plain `git push`
- use `git-drs` for remote configuration, tracking rules, object hydration, object registration/upload, and tracked-file delete reconciliation

The important command split is:

- `git pull` updates Git history and checkout state
- `git drs pull` hydrates tracked pointer files already present in the checkout
- `git drs push` performs the managed data push path: metadata registration, upload when needed, delete reconciliation, and then the Git push flow
- plain `git push` is plain Git only

## Current Package Ownership

The current codebase is organized around a few clear owners:

- `cmd/...`
  - CLI entrypoints and command-local workflow
  - commands may still own substantial logic when that logic is specific to one command
- `internal/transfer`
  - upload/download execution
  - pull/push progress rendering
  - delete reconciliation derived from pushed Git history
- `internal/filter`
  - clean/smudge logic
  - long-running Git filter-process protocol handling
  - skip-smudge behavior
- `internal/lookup`
  - scoped DRS object lookup and record matching
- `internal/gitrepo`
  - repo-local Git wiring
  - `.gitattributes` tracking manipulation
  - repo-local path/config helpers
- `internal/remoteruntime`
  - live remote runtime/client assembly from stored config
  - credential refresh/bootstrap needed to build a working client context
- `internal/config`
  - persisted repo config model and config I/O
- `internal/precommit_cache`
  - pre-commit cache layout, JSON types, and shared cache-path helpers
  - not the owner of hook policy

## Repository-Local State

`git-drs` keeps local state under `.git/drs/`.

The important split is:

- `.git/drs/lfs/objects`
  - authoritative local DRS metadata objects
  - consumed directly by commands such as `git drs push` and `git drs query`
- `.git/drs/pre-commit`
  - rebuildable local cache for path/OID/external URL bookkeeping
  - currently written by `git drs precommit` and `git drs add-url`

The pre-commit cache is non-authoritative and safe to rebuild. The local DRS metadata objects are the real local source of truth.

## Current Workflow Paths

### Setup path

`git drs remote add gen3 ...` or `git drs remote add local ...` is the standard entrypoint for connecting a repository.

That path:

- stores or refreshes remote config
- prepares credentials for the selected runtime
- bootstraps repo-local `git-drs` wiring when it is missing

You can still run `git drs init` explicitly, but the normal onboarding path is `remote add`.

### Push path

`git drs push`:

- discovers local pointer/object metadata
- looks up existing scoped records
- registers missing metadata
- uploads missing payload bytes when local content exists
- reconciles committed tracked-file deletes from the Git ref delta
- then completes the Git push flow

The managed push path runs directly through the current Syfon client/runtime stack.

### Pull path

`git drs pull`:

- scans tracked pointer files in the current checkout
- resolves matching DRS records for the configured remote scope
- downloads payload bytes into the local cache/object area
- hydrates worktree files

It does not run `git pull`.

### Filter path

Git clean/smudge/filter-process integration is handled by the current filter stack:

- `git-drs clean`
- `git-drs smudge`
- `git-drs filter`

These commands use `internal/filter` plus `internal/transfer` rather than an old custom transfer-agent model.

## Pre-Commit Ownership

The current hook story is narrower than older versions of the repo:

- `git drs precommit`
  - reads staged Git content only
  - updates the rebuildable `.git/drs/pre-commit` cache
  - does not perform network I/O
- `git drs add-url`
  - writes a pointer file
  - writes authoritative local DRS metadata
  - updates the pre-commit cache for local coherence

Neither plain `git push` nor `git drs push` depends on the pre-commit cache for remote registration.

## Development Notes

### Building

```bash
go build
```

### Compile-oriented sweep

```bash
GOCACHE=$(pwd)/.gocache go test ./... -run TestDoesNotExist -count=1 -vet=off
```

### Lint

```bash
GOCACHE=$(pwd)/.gocache make lint
```

### Real-repo verification

When debugging behavior, validate the actual workflow split:

```bash
git drs remote list
git drs ls-files
git drs ls-files --drs
git drs pull --dry-run
```

That usually distinguishes:

- remote/config issues
- tracking issues
- hydration issues
- DRS registration issues
