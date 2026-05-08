# ADR 0004: `git drs rm` as scoped Git-plus-remote delete workflow

## Status
Implemented

## Context

`git-drs` currently has a gap in normal file lifecycle handling:

- users can add, register, and push tracked data
- users can remove pointer files from Git with `git rm`
- but removing the file from the repository does not provide a coherent remote deletion workflow

There are older hidden delete-oriented commands, but they are not a good user-facing model:

- they are backend-centric
- they are not path-oriented
- they are easy to use unsafely
- they do not fit the normal repository workflow

At the same time, automatically deleting remote objects as soon as a tracked file disappears from Git is too aggressive:

- one content object may be referenced by multiple DRS records
- one DRS record may exist in more than one project or instance
- deleting a DRS record is not the same as deleting the underlying bucket object
- destructive behavior during `push` must be explicit and scoped

## Decision

Adopt `git drs rm` as the user-facing delete workflow, with three distinct layers of behavior:

1. remove the tracked pointer from the Git repository
2. remove matching remote DRS record state for the configured remote scope during `git drs push`
3. only delete underlying object bytes when explicitly requested by a stronger policy

The canonical behavior is:

- `git drs rm <path>` removes the tracked file from the worktree and index
- it does not write sidecar delete state
- `git drs push` and the managed `pre-push` hook derive deletions from pushed Git ref deltas
- default remote action is **record deletion only**, scoped to the configured organization/project
- bucket object deletion is **not** the default behavior

## Rationale

This model matches the way users already think:

- `git rm` removes a file from the repository
- `git drs push` synchronizes repository state with remote DRS state

It also avoids the worst failure mode:

- silent deletion of shared underlying object bytes

Separating record deletion from object-byte deletion keeps the default behavior aligned with least surprise and least destruction.

It also avoids extra small-file local I/O under `.git/...`, which is a poor fit for HPC-style environments.

## Detailed semantics

### Local command behavior

`git drs rm <path>...` should:

- validate that each path is tracked as a git-drs/LFS pointer
- remove the file from the index and worktree
- fail clearly for plain Git files

### Push behavior

When `git drs push` or the managed `pre-push` hook reconciles deletes, it should:

- resolve the current remote and configured organization/project scope
- compute deleted paths from the pushed Git ref delta
- read the deleted pointer blob from the old tree
- parse the tracked object identity from that pointer
- resolve the tracked object identity to matching remote DRS records in that scope
- delete matching record state when the result is unambiguous
- warn and require explicit follow-up when the result is ambiguous

Examples of ambiguity:

- no matching record exists in the configured scope
- more than one matching record exists in the configured scope
- the object is shared with another project or instance and the server cannot prove safe purge semantics

### Default delete policy

Default policy for `git drs rm` + `git drs push`:

- remove the Git pointer locally
- if the scoped record has one `controlled_access` entry, delete the whole record
- if the scoped record has multiple `controlled_access` entries, remove only the current scope resource
- do not delete bucket bytes automatically

### Optional stronger policy

A future explicit mode may support purging underlying bytes, for example:

- `git drs rm --purge-object <path>`
- repo config enabling scoped auto-purge

That mode must remain opt-in and should only proceed when the server can prove the delete is safe or the user explicitly forces it.

## Non-goals

This ADR does not define:

- cross-project garbage collection
- global deduplication ownership policy
- automatic deletion of all records sharing the same checksum across an instance
- silent purge of shared bucket content

## Implementation direction

### Phase 1: user-facing command

Add `git drs rm` as a first-class command:

- path-oriented
- repo-aware
- explicit about remote consequences

### Phase 2: push-time sync

Teach `git drs push` to:

- derive deleted tracked pointers from pushed refs
- apply scoped record deletion
- print a concise summary of deleted records, skipped records, and ambiguous cases

### Phase 3: optional purge policy

Add a stricter opt-in mode for underlying object-byte deletion with strong safeguards.

## Consequences

### Positive

- delete workflow becomes part of the normal `git-drs` lifecycle
- users no longer need to manually reconcile Git state and remote DRS state
- destructive bucket deletion is kept behind explicit policy

### Negative

- push flow must inspect Git history carefully
- delete semantics require server-side and client-side ambiguity handling
- `git drs push` needs an explicit compare base when it is not running under the `pre-push` hook

## Current notes

- `git drs rm` wraps `git rm` directly after validating tracked LFS/git-drs paths.
- Plain `git push` uses the managed `pre-push` hook, which receives authoritative old/new SHAs from Git.
- `git drs push` derives deletes from `HEAD` vs `@{upstream}` when an upstream exists.
- Ambiguous remote matches warn and remain untouched.
