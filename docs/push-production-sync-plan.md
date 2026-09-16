# Production Git/DRS Push Synchronization Plan

## Status

Proposed implementation plan for replacing the current full-tip scan and unconditional metadata upsert behavior in `git drs push`.

This plan defines the production contract before implementation. It deliberately separates Git history discovery, remote synchronization state, DRS transfer negotiation, payload transfer, Git ref updates, and garbage collection.

## Problem statement

The current push path:

1. Assumes one local `HEAD` update.
2. Scans every pointer in the pushed tip tree.
3. Looks up every discovered OID in Syfon.
4. Builds a metadata record for every discovered OID, including records that already match the project scope.
5. Sends all metadata records in one bulk request.
6. Deletes scoped metadata when a pointer disappears from the tip tree.
7. Prints unconditional `DEBUG:` messages to stderr.

This behavior is not proportional to the work being pushed. It also does not model all LFS-style objects in Git history: it models only the final tree, while deleting metadata for objects that can remain reachable from earlier commits.

The replacement must behave like Git and Git LFS:

- Git refs and the Git object graph define what history exists remotely.
- Every valid LFS-style pointer reachable from retained remote refs is part of the synchronization contract.
- Content-addressed OIDs, not paths or pushes, are the transfer unit.
- Shared remote state records which Git histories have been completely reconciled with DRS.
- The difference between Git reachability and DRS-acknowledged reachability is the pending work queue.
- Syfon decides which content-addressed objects require registration, upload, or no action.
- A plain `git push` can advance Git without advancing DRS acknowledgment; a later `git drs push` must recover the gap from any clone.

## Required invariants

Implementation is not complete until these invariants hold.

### History completeness

For every acknowledged Git commit, every valid LFS-style pointer blob reachable through that commit must have a satisfied DRS object for the configured scope.

This includes pointers found in:

- the current tree;
- earlier commits;
- files later renamed or deleted;
- merged histories;
- pushed branches;
- pushed annotated and lightweight tag targets.

A path disappearing from the current tree does not make its historical OID unsynchronized.

### Remote authority

Synchronization acknowledgment must be shared remote state. A local cache, local timestamp, or “last push from this clone” marker may improve performance, but it must never be authoritative.

Multiple clones and users must compute the same pending history from the same remote state.

### Content-addressed idempotency

Repeated planning, registration, and upload attempts for the same `(algorithm, OID, scope)` must be safe.

A retry after partial success must discover that already completed work requires no action.

### No false acknowledgment

A locally available payload must not be marked synchronized while it is:

- missing required project metadata;
- still uploading;
- rejected by Syfon;
- unresolved because its pointer format is unsupported.

Reachable pointers whose payload bytes are unavailable in the current checkout
are outside this invocation's actionable set. They must be skipped without
creating orphan metadata or blocking locally available uploads.

### Ref-specific progress

Acknowledgment is tracked per pushed branch or tag, not once per repository and not once per invocation.

### Bounded network requests

All lookup, negotiation, registration, and upload planning requests must have explicit record-count and/or byte-size bounds. Repository size must never become a single HTTP request size.

### Push is not garbage collection

Normal push synchronization must not delete a DRS record merely because a file is absent from a new tip tree. Cleanup requires a separate reachability and retention decision across all retained refs.

## Architecture

### 1. Git ref update set

Replace `currentPushRefUpdates`, which synthesizes one `HEAD` update, with refspec-aware push negotiation.

The push planner must resolve the same updates that the eventual Git push will attempt:

```text
local ref name
local object ID
remote ref name
advertised remote object ID
update kind: create | fast-forward | force | delete | unchanged
```

The implementation must support:

- the configured upstream when no refspec is supplied;
- explicit branches;
- multiple refspecs;
- new branches;
- tags;
- deleted refs;
- force pushes only when the user explicitly requested Git force semantics.

The first implementation may keep the current `git drs push [remote]` CLI surface, but internal types must accept multiple ref updates so later refspec arguments do not require another redesign.

Suggested package:

```text
internal/gitpush
    negotiate.go
    refs.go
    command.go
```

Suggested core type:

```go
type RefUpdate struct {
    LocalRef  string
    LocalOID  string
    RemoteRef string
    RemoteOID string
    Force     bool
    Delete    bool
}
```

All object IDs used for one run must be captured before discovery begins. If a local source ref changes during the operation, abort before acknowledgment rather than acknowledging a different graph.

### 2. Remote DRS synchronization ledger

Add an abstraction for authoritative acknowledgment state:

```go
type SyncLedger interface {
    GetAcknowledgments(ctx context.Context, remote string, refs []string) ([]Acknowledgment, error)
    CompareAndAdvance(ctx context.Context, updates []AcknowledgmentUpdate) error
}
```

An acknowledgment maps a Git target ref to a Git object ID whose entire reachable LFS-style object closure has been reconciled.

#### Initial backend: protected remote Git refs

Use a dedicated remote namespace:

```text
refs/git-drs/synced/heads/main
refs/git-drs/synced/heads/release
refs/git-drs/synced/tags/v1.2.3
```

These are remote acknowledgments, not local cursors. The pending graph for a target is derived from the target ref and its remote acknowledgment ref.

Acknowledgment updates must use compare-and-swap/lease semantics. A client must never overwrite an acknowledgment value it did not plan against.

Before selecting this backend for a deployment, verify that the Git server:

- advertises the namespace;
- permits fetches of the namespace;
- supports protected writes or a trusted service path;
- does not allow ordinary users to forge synchronization acknowledgments;
- supports atomic or leased updates.

Client-writable acknowledgment refs are acceptable for an initial compatibility implementation but are not a sufficient trust boundary for a hardened deployment. The package interface must permit replacement with a server-managed ledger.

#### Production backend: server-managed ledger

The production design should support a Git server or coordination service that:

1. Observes accepted ref updates through the Git server's receive path.
2. Records pending `(repository, ref, old OID, new OID)` work.
3. Exposes acknowledgment state to `git-drs`.
4. Advances acknowledgment only after Syfon confirms required metadata and payload availability.

This is the only fully authoritative way to detect plain `git push` activity while preventing clients from forging completion. The `SyncLedger` interface lets `git-drs push` use either backend without coupling graph discovery and transfer code to Gecko or another Git host.

If neither remote backend is available, the safe fallback is a full remote-ref reconciliation on every invocation. A local cursor must not be presented as equivalent recovery semantics.

### 3. Pending Git object graph

For each non-deleted target ref, compute Git objects reachable from its desired target OID but not already covered by synchronized histories.

Conceptually:

```bash
git rev-list --objects <desired-targets> --not <acknowledged-targets>
```

Use the union of applicable acknowledgment refs as negative roots so objects already synchronized through another branch are not repeatedly parsed. Preserve per-target coverage information so each acknowledgment advances only after its complete closure is satisfied.

Do not use `git diff --name-only` as the primary discovery algorithm. A tree diff misses historical pointer objects that were introduced and then deleted inside the pending commit range.

Handle cases explicitly:

- No acknowledgment: traverse the complete reachable history for that target.
- Fast-forward: traverse newly reachable objects.
- Merge: traverse both newly reachable parent histories.
- Force push or rebase: treat the desired target closure as authoritative and subtract all known synchronized closures; do not assume ancestry.
- Ref deletion: remove or tombstone its acknowledgment only after the Git deletion succeeds; do not delete DRS content during push.
- Shared commits: parse once and attribute coverage to every relevant target.

Suggested package:

```text
internal/gitobjects
    revlist.go
    batch_reader.go
    pointer_inventory.go
    coverage.go
```

### 4. Pointer blob inventory

Read candidate Git objects using one long-lived `git cat-file --batch` or `--batch-command` process. Do not run `git show` once per object or path.

Filter by object type and size before parsing:

- ignore commits, trees, and tags after recording traversal metadata;
- ignore blobs larger than a conservative pointer-size limit;
- parse small blobs with the canonical pointer parser;
- accept both standard Git LFS SHA-256 pointers and supported Calypr DRS pointer forms;
- retain pointer blob ID, OID type, normalized OID, declared size, and representative commit/path provenance.

Introduce a path-independent identity:

```go
type PointerObject struct {
    OIDType    string
    OID        string
    Size       int64
    BlobOID    string
    References []PointerReference
}
```

Deduplicate transfer work by `(OIDType, normalized OID)`, not path. Keep bounded representative references for actionable errors without retaining every path in memory for very large histories.

The existing `LfsFileInfo` type is path/worktree-oriented and should not become the graph inventory contract. Adapt `PointerObject` to existing transfer code during migration.

### 5. Payload source resolution

Historical pointer discovery and payload discovery are separate operations.

For every SHA-256 pointer requiring upload, resolve payload bytes in this order:

1. The git-drs/Git LFS object cache derived from `internal/lfs.ObjectPath`.
2. A current worktree path only if its size and SHA-256 verify against the pointer.
3. Any explicitly supported configured local object source.

Never assume a historical path still exists in the worktree.

If payload bytes are unavailable:

- omit the object from the registration/upload plan;
- do not create metadata solely for that unavailable payload;
- continue planning and uploading locally available objects;
- retain trace-level provenance so an explicit reconciliation command can inspect it later.

DRS URI pointer forms that already refer to resolvable remote objects should be classified as references rather than local upload candidates.

Suggested package:

```text
internal/payload
    resolve.go
    verify.go
```

### 6. DRS transfer negotiation

Split the current monolithic `BatchSyncForPush` into planning and execution.

```go
type TransferPlanner interface {
    Plan(ctx context.Context, scope Scope, objects []PointerObject) (TransferPlan, error)
}

type TransferPlan struct {
    Satisfied       []PlannedObject
    RegisterOnly    []PlannedObject
    RegisterUpload  []PlannedObject
    UploadOnly      []PlannedObject
    Unresolved      []PlannedObject
}
```

#### Initial implementation using existing APIs

Batch checksum lookups for candidate OIDs only. For each OID:

- matching record already exists in the current org/project and has acceptable access information: `Satisfied`;
- reusable object exists outside the current scope: `RegisterOnly`;
- scoped metadata exists but required payload is unavailable remotely: `UploadOnly`;
- no reusable object exists: `RegisterUpload`;
- ambiguous or unsupported state: `Unresolved`.

The existing matching-project branch in `ensureMetadataRegistered` must stop appending to `toRegister`.

#### Extensible batch negotiation endpoint

Define a future Syfon batch contract modeled after Git LFS batch negotiation. The request supplies scope and content-addressed objects; the response supplies required actions and action-specific upload details.

The client interface should allow this endpoint to replace detailed metadata lookups without changing Git graph discovery or execution.

All batches must be bounded by both:

- maximum object count; and
- maximum serialized request bytes.

Use conservative defaults and permit server-advertised limits later.

### 7. Registration and upload execution

Execute only actions returned by the transfer plan.

Registration requirements:

- send bounded batches, initially no more than 250 records;
- split earlier when the serialized JSON body reaches the configured byte limit;
- update progress after each successful batch;
- retain successful partial work when a later batch fails;
- rely on re-planning for retry rather than maintaining a client-side rollback log.

Upload requirements:

- keep bounded concurrency for small objects;
- retain sequential or explicitly bounded multipart behavior for large objects;
- verify the payload against its pointer before upload;
- propagate cancellation;
- treat “already exists” as success only after the server confirms content identity;
- never mark an object satisfied solely because metadata exists.

After execution, revalidate unresolved action results when the API does not provide a definitive completion receipt.

### 8. Git push and acknowledgment ordering

The orchestration sequence must be:

1. Acquire the remote Git ref advertisement and synchronization acknowledgments.
2. Resolve the intended Git ref updates.
3. Capture immutable local target OIDs.
4. Discover pending pointer objects.
5. Negotiate and execute required DRS actions.
6. Refuse to continue if any target's pointer closure is incomplete.
7. Execute the real Git push with the exact planned refspecs and force semantics.
8. Refresh the remote ref advertisement.
9. Confirm which planned target OIDs were accepted.
10. Advance only the corresponding synchronization acknowledgments using leases.

Failure behavior:

- DRS failure before Git push: Git refs remain unchanged; acknowledgments remain unchanged.
- Git push rejection after DRS success: extra DRS objects are harmless; acknowledgments remain unchanged.
- Git push success followed by acknowledgment failure: Git is ahead; the next client detects and safely re-plans the gap.
- Partial multi-ref Git push: acknowledge only refs proven accepted and completely reconciled.
- Concurrent branch advance: acknowledge the exact accepted OID, not the newly observed tip.

Where supported, use Git atomic push for multi-ref operations. If unavailable, report per-ref results accurately.

### 9. Delete and retention semantics

Remove `ReconcileCommittedDeletes` from the normal push path in its current form.

Its current tip-diff behavior can delete metadata for an OID that remains reachable from earlier Git history, violating the all-LFS-object invariant.

Replace push-time deletion with two separate concepts:

- Ref acknowledgment cleanup: when a Git ref is deleted, delete/tombstone only its synchronization acknowledgment.
- Explicit DRS garbage collection: determine OIDs unreachable from all retained Git refs and apply repository retention policy before removing project metadata or payloads.

Garbage collection is a separate command or server job and is out of scope for the first push implementation. Until it exists, preserving extra DRS objects is safer than destroying historically reachable data.

Update `git drs rm` documentation accordingly: removing a path and pushing removes it from the Git tip but does not immediately destroy its historical DRS object.

### 10. Output and observability

Delete unconditional `fmt.Fprintln(..., "DEBUG: ...")` calls from `cmd/push` and `internal/transfer`.

Default output must contain only:

- actionable warnings;
- one discovery/plan summary;
- progress for actual registration or upload work;
- one final per-ref or aggregate result;
- errors that explain how to recover.

Example normal push:

```text
DRS: 12 new pointer objects across 2 refs; 9 already available, 2 to register, 1 to upload
Registering metadata: 2/2
Uploading objects: 1/1
Git: pushed main and v1.4.0 to origin
DRS: synchronized 2 refs
```

Example no-op:

```text
DRS: all reachable LFS objects are synchronized
Git: everything up to date
```

Example missing historical payload:

```text
DRS: cannot synchronize 1 object because payload bytes are unavailable locally
  sha256:<oid> size=<bytes> referenced by <commit>:<path>
Git refs were not pushed. Restore the object cache or run this push from a clone that has the payload.
```

Detailed Git commands, object IDs, batch numbers, and per-object decisions belong behind `GIT_TRANSFER_TRACE=1` and structured `slog` fields. Existing token-expiration warnings remain visible.

### 11. Resource limits

Define and test limits rather than relying on repository size:

- Git object traversal streams output; it does not load the entire rev-list result into memory.
- Blob reads use one batch process.
- Pointer identities are deduplicated incrementally.
- Reference provenance per OID is capped.
- Lookup and registration requests are count- and byte-bounded.
- Upload concurrency is bounded.
- Progress rendering is O(number of actions), not O(number of Git objects).
- Cancellation terminates Git subprocesses and HTTP work.

Expose advanced tuning only where operationally necessary. Defaults should work without user configuration.

## Code migration map

### `cmd/push/main.go`

- Reduce Cobra `RunE` to orchestration and user-facing error handling.
- Remove direct debug printing.
- Replace `currentPushRefUpdates` and `discoverLfsFilesForPush` with `gitpush` and `gitobjects` services.
- Execute exact planned refspecs.
- Advance remote acknowledgments after confirmed Git success.

### `internal/lfs/inventory.go`

- Keep worktree/tip inventory helpers for `pull` and `ls-files`.
- Move history-wide object traversal into `internal/gitobjects`.
- Export or centralize canonical pointer parsing without exposing path-oriented inventory types.

### `internal/transfer/push.go`

- Split normalization, planning, registration, and upload execution.
- Remove unconditional registration of matching scoped records.
- Accept path-independent pointer objects and resolved payload sources.
- Add bounded registration batching.

### `internal/transfer/delete_reconcile.go`

- Remove from normal push orchestration.
- Preserve temporarily only for an explicitly invoked legacy/admin workflow if required.
- Do not reuse it as historical object garbage collection.

### New packages

```text
internal/gitpush       Git refspec negotiation and exact push execution
internal/gitobjects    reachable object traversal and pointer inventory
internal/syncledger    remote acknowledgment backends and leases
internal/payload       local payload discovery and verification
internal/pushplan      cross-component orchestration and per-ref coverage
```

Avoid a single new oversized push file. Keep Git mechanics independent from Syfon API mechanics.

## Delivery phases

### Phase 0: protocol and capability spike

- Verify custom remote ref behavior against every supported Git host used by git-drs.
- Decide whether the first ledger backend is protected Git refs or requires a server endpoint.
- Record the acknowledgment trust model.
- Capture representative ref advertisements and force-push behavior in tests.

Exit criteria: the team agrees on an authoritative remote ledger backend and its security properties.

### Phase 1: history-complete pointer inventory

- Implement streaming `rev-list` traversal.
- Implement batch object reads.
- Parse all supported LFS-style pointers from pending history.
- Resolve historical payloads from the object cache.
- Add no-network repository integration tests.

Exit criteria: given desired and negative roots, the inventory returns every newly reachable pointer OID, including add-then-delete and merged-history cases.

### Phase 2: transfer planning correctness

- Introduce `TransferPlan` classifications.
- Stop rewriting matching project records.
- Add bounded lookup and registration batches.
- Add payload verification and unresolved-object reporting.

Exit criteria: 4,338 existing OIDs produce zero registration writes; 4,338 missing OIDs produce bounded requests and no HTTP 413-sized body.

### Phase 3: remote ledger and multi-ref orchestration

- Implement `SyncLedger`.
- Make push negotiation refspec-aware.
- Connect per-ref coverage to acknowledgment advancement.
- Add lease/CAS conflict handling.

Exit criteria: a plain Git push from clone A is recovered by `git drs push` from clone B, and only fully satisfied refs are acknowledged.

### Phase 4: replace current command path

- Wire the new planner into `cmd/push`.
- Remove current full-tip scan from the normal path.
- Remove current push-time delete reconciliation.
- Add concise production output and trace-only diagnostics.

Exit criteria: the old `BatchSyncForPush` orchestration is no longer used by the command, and default output contains no unconditional `DEBUG:` lines.

### Phase 5: hardening and rollout

- Run large-history, high-OID-count, multi-branch, force-push, retry, and concurrency tests.
- Add metrics for planning time, candidate count, actions, bytes, retries, and acknowledgment conflicts.
- Document recovery procedures.
- Release behind an opt-in feature flag for real repository validation.
- Compare results against a full reconciliation audit.
- Make the new path default only after parity is proven.

Exit criteria: audit finds no reachable unsatisfied pointer after acknowledged pushes, and interrupted operations recover without manual state repair.

## Test plan

### Unit tests

- Pointer parser accepts standard SHA-256 and supported DRS forms.
- Pointer parser rejects malformed and oversized blobs.
- OID deduplication ignores path duplication.
- Batch-size and serialized-byte limits are enforced.
- Matching scoped records produce `Satisfied`, not registration.
- Cross-scope reusable records produce `RegisterOnly`.
- Missing payload produces `Unresolved` and blocks coverage.
- Acknowledgment leases reject stale expected values.

### Git graph integration tests

- Pointer added in the current tip.
- Pointer added and deleted within the pending range.
- Pointer renamed multiple times.
- Two paths reference the same OID.
- Two branches share commits and OIDs.
- Merge introduces pointers from a side branch.
- Annotated tag reaches pointer history.
- Branch is force-pushed to unrelated history.
- Branch and tag are pushed together.
- Remote ref is deleted.
- No acknowledgment causes full history traversal.
- Existing acknowledgments exclude already covered objects.

### Distributed recovery tests

- Clone A runs plain `git push`; clone B recovers with `git drs push`.
- Clone A uploads but fails before Git push; clone B sees no false acknowledgment.
- Git push succeeds but acknowledgment update fails; next invocation recovers.
- Two clients race to synchronize the same ref.
- Two clients synchronize different refs sharing OIDs.
- One client has a missing payload and another completes it.

### Scale and failure tests

- 100,000 commits with bounded memory traversal.
- 100,000 pointer references with substantial OID deduplication.
- 4,338 and larger registration sets with bounded HTTP bodies.
- HTTP 413, 429, 500, timeout, and connection-reset behavior.
- Process cancellation during rev-list, registration, small upload, and multipart upload.
- Partial registration batch success followed by failure and retry.
- Corrupt cache payload whose bytes do not match its OID.

### Output contract tests

- No `DEBUG:` output without trace enabled.
- No progress bar for zero actions.
- Missing payload output identifies a recoverable OID and provenance.
- Partial multi-ref results name successful and failed refs.
- Trace output never includes credentials or signed URLs.

## Production acceptance criteria

The new push path is production-ready when all of the following are demonstrated:

1. Every acknowledged ref has complete DRS coverage for all reachable LFS-style pointer objects.
2. A plain Git push is recoverable from another clone without local cursor state.
3. Normal pushes inspect only Git objects not covered by remote acknowledgments.
4. Already satisfied scoped records are not rewritten.
5. Network request sizes remain bounded for large repositories.
6. Missing historical payloads do not block locally available uploads or create orphan metadata.
7. Concurrent clients cannot silently lose pending work.
8. Push does not delete historically reachable DRS records.
9. Multi-ref and force-push behavior matches the planned Git ref updates.
10. Default output is concise; detailed diagnostics require trace mode.
11. Interrupted operations are safely retryable.
12. A full reconciliation audit agrees with the remote acknowledgment ledger before the feature becomes the default.

## Explicit non-goals for the first release

- Automatic deletion of unreachable DRS payloads.
- A complete repository retention-policy engine.
- Replacing Git's transport or object database.
- Treating local worktree state as authoritative.
- Updating unrelated metadata on every push.
- Hiding unresolved historical payloads by advancing acknowledgments anyway.

Those capabilities can be added behind the Git graph, ledger, and transfer-plan interfaces without changing the core synchronization model.
