# ADR 0004: `git drs rm` as scoped Git-plus-remote delete workflow

## Status
Proposed

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
- it records deletion intent for the tracked object in repo-local git-drs state
- `git drs push` applies that deletion intent against the configured remote
- default remote action is **record deletion only**, scoped to the configured organization/project
- bucket object deletion is **not** the default behavior

## Rationale

This model matches the way users already think:

- `git rm` removes a file from the repository
- `git drs push` synchronizes repository state with remote DRS state

It also avoids the worst failure mode:

- silent deletion of shared underlying object bytes

Separating record deletion from object-byte deletion keeps the default behavior aligned with least surprise and least destruction.

## Detailed semantics

### Local command behavior

`git drs rm <path>...` should:

- validate that each path is tracked as a git-drs/LFS pointer
- remove the file from the index and worktree
- record repo-local deletion intent keyed by tracked object identity and path
- fail clearly for plain Git files unless explicitly told to delegate to normal `git rm`

### Push behavior

When `git drs push` sees recorded deletion intent, it should:

- resolve the current remote and configured organization/project scope
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
- delete matching DRS record(s) in current scope only
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

- read pending deletion intent
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

- push flow becomes more stateful
- delete semantics require server-side and client-side ambiguity handling
- repo-local deletion intent must be durable and recoverable across interrupted workflows

## Open questions

- What exact repo-local format should store pending deletion intent?
- Should `git drs rm` wrap `git rm` directly, or should it stage deletion through a git-drs-managed preflight path?
- Should ambiguous delete cases block `git drs push`, or warn and continue with non-delete uploads?
- What server contract is required to safely support future object-byte purge semantics?
