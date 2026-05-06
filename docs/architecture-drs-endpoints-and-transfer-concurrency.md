# Architecture: DRS Endpoint Flows, Transfer Concurrency, and `add-url`/`add-ref`

This document explains three implementation areas in `git-drs`:

1. How user-issued Git/Git-DRS commands map to GA4GH DRS endpoint calls.
2. How transfer concurrency works for upload and download.
3. How `add-url` and `add-ref` work, including when and where SHA existence is checked on the DRS server.

---

## 1) Command to Endpoint Trace (User command -> Code path -> DRS API)

## 1.1 High-level command routing

- User-facing commands are registered in `cmd/root.go`.
- Relevant command entrypoints:
  - `git drs push` -> `cmd/push/main.go`
  - `git drs pull` -> `cmd/pull/main.go`
  - `git drs download` -> `cmd/download/main.go`
  - `git drs query` -> `cmd/query/main.go`
  - `git drs list` -> `cmd/list/main.go`
  - `git drs add-ref` -> `cmd/addref/add-ref.go`
  - `git drs add-url` -> `cmd/addurl/service.go`

`git-drs` obtains a remote-specific API client via `config.LoadConfig()` + `cfg.GetRemoteClient(...)` (see `internal/config/remote.go`).

## 1.2 Endpoint mapping matrix

The table below maps command behavior to DRS client calls and the corresponding DRS API intent.

| User command | Main call path | DRS client method(s) | DRS endpoint intent |
| --- | --- | --- | --- |
| `git drs query <drs_id>` | `cmd/query/main.go` | `DRS().GetObject(drs_id)` | Get object by DRS ID (`/ga4gh/drs/v1/objects/{id}` style) |
| `git drs query --checksum <sha256>` | `cmd/query/main.go` -> `drsremote.ObjectsByHashForScope` | `DRS().BatchGetObjectsByHash([]checksum)` | Lookup objects by checksum (`/ga4gh/drs/v1/objects/checksum/{checksum}` style; asserted in tests) |
| `git drs list` | `cmd/list/main.go` | `DRS().ListObjects(pageSize, page)` | List objects (`/ga4gh/drs/v1/objects` style) |
| `git drs download <drs_id>` | `cmd/download/main.go` | `DRS().GetObject(id)` then `DRS().GetAccessURL(id, access_type)` | Resolve object metadata and a signed access URL |
| `git drs pull [remote]` | `cmd/pull/main.go` -> `drsremote.DownloadToCachePath` | `DRS().BatchGetObjectsByHash`, `DRS().GetAccessURL`; optional bulk via `DRSAPI().GetBulkAccessURLWithResponse` | Resolve missing OIDs to DRS records and access URLs, then download content |
| `git drs push [remote]` | `cmd/push/main.go` -> `pushsync.BatchSyncForPush` | `DRS().BatchGetObjectsByHash`, `DRS().RegisterObjects`, `DRS().GetAccessURL` | Check checksum presence, register missing records, probe/downloadability before upload |
| `git drs add-ref <drs_uri> <path>` | `cmd/addref/add-ref.go` | `DRS().GetObject(drs_uri)` | Resolve existing DRS object and write pointer |

Notes:

- `internal/drsremote/remote_test.go` explicitly verifies some concrete paths:
  - checksum lookup path `/ga4gh/drs/v1/objects/checksum/{sha}`
  - bulk access path `/ga4gh/drs/v1/objects/access`
  - access URL path `/ga4gh/drs/v1/objects/{id}/access/{type}`
- `git drs pre-push-prepare` also calls a non-GA4GH metadata staging endpoint:
  - `POST {remote}/info/lfs/objects/metadata` (`cmd/prepush/main.go`)
  - This is optional capability and not part of GA4GH DRS.

## 1.3 Trace from standard Git commands

`git-drs` participates in both explicit `git drs ...` commands and standard Git workflows after `git drs init`:

- `git drs init` installs hooks (`cmd/initialize/main.go`):
  - pre-commit: `git drs precommit`
  - pre-push: `git drs pre-push-prepare`
- During a normal `git push`, pre-push metadata can be staged via `/info/lfs/objects/metadata` before transfer.
- The explicit `git drs push` command runs the register/upload workflow, then runs `git push --no-verify` by default (`cmd/push/main.go`).

---

## 2) Transfer Concurrency Model (Upload and Download)

### Concurrency mechanism: in-process goroutines only

All transfer concurrency in `git-drs` is **in-process**, implemented with **Go goroutines and channels**. There is no use of OS-level multi-processing (no `fork`/`exec` of worker processes) for data movement.

- Upload object fan-out uses `golang.org/x/sync/errgroup` — goroutines with a shared context and bounded by `errgroup.SetLimit(n)`.
- Download chunk parallelism uses the `sydownload` library, which internally uses goroutines to issue concurrent HTTP range requests.
- Sub-process calls (`exec.Command("git", ...)`, `exec.Command("git", "lfs", ...)`) appear only for Git/LFS metadata operations (e.g. `git pull`, `git lfs checkout`, `git lfs ls-files`), never for data-transfer concurrency.

## 2.1 Upload concurrency (`git drs push`)

Upload tuning originates from Git config and is carried in `config.GitContext`:

- `lfs.concurrenttransfers` -> `UploadConcurrency`
- `drs.multipart-threshold` (MB) -> `MultiPartThreshold`

See `internal/config/remote.go` (`newGitContext`) and `cmd/initialize/main.go` (`initGitConfig`).

### Upload execution strategy

In `internal/pushsync/batch_sync.go`:

1. Build upload candidates.
2. Split candidates into:
   - small files: `size < MultiPartThreshold`
   - large files: `size >= MultiPartThreshold`
3. Small files upload in parallel using `errgroup.WithContext` + `eg.SetLimit(UploadConcurrency)` + `eg.Go(goroutine)` — **in-process goroutine fan-out**.
4. Large files upload sequentially (single goroutine, no additional concurrency).

Key implementation points:

- `executeUploadPlan(...)` controls fan-out and limits.
- Actual upload call is `syupload.UploadObjectFile(...)` in `internal/pushsync/register.go`.
- `forceMultipart` is computed per file (`fileSize >= threshold`) and passed to upload.

Operationally, this gives bounded goroutine parallelism for many small objects while reducing resource contention for very large uploads.

## 2.2 Download concurrency (`git drs pull` and `git drs download`)

Download concurrency is set via `sydownload.DownloadOptions`:

- `MultipartThreshold: 5 MiB`
- `Concurrency: 2`
- `ChunkSize: 64 MiB`

These values are hardcoded in two places (see section 6.2):

- `internal/drsremote/remote.go` (`downloadResolved`) — used by pull workflow
- `cmd/download/main.go` — used by the explicit download command

This applies to:

- `DownloadResolvedToPath(...)` (direct download command)
- `DownloadToCachePath(...)` / `DownloadResolvedToCachePath(...)` (pull workflow)

### Intra-object chunk concurrency

The `sydownload` library implements **goroutine-based HTTP range-request concurrency** within a single object download:

- `resolvedSource.GetRangeReader(ctx, guid, offset, length)` issues an HTTP range (`Range: bytes=offset-end`) request.
- `sydownload.DownloadToPathWithOptions` coordinates up to `Concurrency` (2) goroutines issuing simultaneous range requests per object.
- This is purely in-process; no subprocess is spawned.

### Object-level iteration in pull

- In `cmd/pull/main.go`, missing OIDs are processed in a **sequential** `for` loop — one object at a time.
- Each object download can still be internally chunk-concurrent (up to `Concurrency=2` goroutines) via `sydownload`.
- So pull concurrency is **intra-object** (goroutine-based chunk/range concurrency), not broad object fan-out.
- Bulk metadata prefetch (DRS objects + bulk access URLs) is performed **before** the sequential download loop to amortize API round-trips.

## 2.3 Git LFS concurrency lane

Some flows still call Git LFS directly (for example `git drs fetch` runs `git lfs pull` in `cmd/fetch/main.go`).

- These are **subprocess** calls (`exec.Command("git", "lfs", ...)`), not goroutine fan-out.
- That lane uses Git LFS runtime behavior and respects `lfs.concurrenttransfers` in Git config.
- Git LFS own transfer concurrency is managed within the `git-lfs` subprocess and is not visible to `git-drs`.
- This is distinct from the goroutine-based `git drs push` upload fan-out and `sydownload` chunk concurrency.

---

## 3) `add-url` and `add-ref`: Implementation and SHA existence checks

## 3.1 `add-url` implementation

Main logic lives in `cmd/addurl/service.go`.

Workflow:

1. Parse CLI input (`cmd/addurl/params.go`).
2. Resolve remote scope (org/project/bucket/prefix) (`cmd/addurl/scope.go`).
3. Resolve source object URL (full URL mode or key+`--scheme` mode).
4. Inspect object using cloud client (`sycloud.InspectObject`).
5. Ensure LFS object identity:
   - If `--sha256` provided: trust it as OID.
   - Otherwise: derive synthetic OID from ETag and write sentinel object (`lfs.SyntheticOIDFromETag`, `lfs.WriteAddURLSentinelObject`).
6. Write LFS pointer file to worktree.
7. Best-effort update of pre-commit cache (`updatePrecommitCache`).
8. Ensure file is tracked by LFS if needed.
9. Write/update local DRS metadata object under `.git/drs/lfs/objects` (`writeAddURLDrsObject`).

### Does `add-url` query DRS server for SHA existence?

Not immediately. `add-url` is local-preparation oriented:

- It inspects provider object metadata.
- It writes local pointer + local DRS metadata.
- Server checksum existence is checked later during push (see section 3.3).

## 3.2 `add-ref` implementation

Main logic is in `cmd/addref/add-ref.go`.

Workflow:

1. Resolve remote client.
2. Call `DRS().GetObject(drs_uri)`.
3. Create parent directory if needed.
4. Write Git LFS pointer from returned DRS object checksums (`lfs.CreateLfsPointer`).

### Does `add-ref` query DRS server for SHA existence?

It does not perform a checksum lookup endpoint call. It verifies existence by object ID (`GetObject`) and consumes checksum from that object payload.

## 3.3 Where SHA existence check against DRS actually happens

Checksum existence checks are performed during `git drs push` in `internal/pushsync/batch_sync.go`:

1. `lookupMetadata()` iterates OIDs and calls:
   - `drsremote.ObjectsByHash(...)` -> `DRS().BatchGetObjectsByHash(...)`
2. If no records exist for an OID, object candidate is included for bulk registration:
   - `DRS().RegisterObjects(...)`
3. Upload decision is then based on registration status + downloadability probe.

So for both `add-url` and `add-ref`, the checksum-existence gate is primarily deferred to push-time synchronization logic.

---

## 4) End-to-end sequence summaries

## 4.1 `git drs add-url ...` then `git drs push`

1. `add-url`: local pointer + local DRS object prepared.
2. `push`: checksum lookup (`BatchGetObjectsByHash`).
3. Missing checksum -> `RegisterObjects`.
4. If payload required and available -> upload via syfon transfer.
5. Git refs pushed.

## 4.2 `git drs add-ref <drs_id> <path>` then `git drs pull`

1. `add-ref`: `GetObject(drs_id)` and write pointer.
2. `pull`: detect unresolved pointers.
3. For each OID, resolve scoped object by checksum and access URL.
4. Download to LFS cache; checkout file contents.

---

## 5) Practical implications for operators and developers

- If you need immediate server-side checksum validation during `add-url`, that behavior does not exist today; validation happens at push time.
- All transfer concurrency is in-process (goroutines); no subprocess workers are used for data movement.
- Upload concurrency is configurable through Git config (`lfs.concurrenttransfers`) and is implemented as a goroutine pool bounded by `errgroup.SetLimit`.
- Download concurrency is fixed (not configurable at runtime): `Concurrency=2` goroutines per object for HTTP range requests, hardcoded in two places (`internal/drsremote/remote.go` and `cmd/download/main.go`).
- Object-level download iteration in `git drs pull` is sequential; only intra-object chunk downloads are concurrent.
- `git drs fetch` delegates entirely to the `git lfs pull` subprocess; its concurrency is controlled by Git LFS runtime, not by `git-drs` goroutine management. Tuning and diagnostics differ accordingly.

---

## 6) Deprecation Candidates

These are candidates for cleanup/refactor, based on current behavior and code layout.

## 6.1 Outdated / legacy LFS-oriented paths

- `cmd/fetch/main.go` still shells out to `git lfs pull` directly. This is a legacy-style path compared with the managed DRS pull path in `cmd/pull/main.go`.
- `cmd/pull/main.go` long help string says "git pull, git lfs pull, git lfs checkout", but implementation now uses DRS checksum lookup + direct download + `git lfs checkout` fallback. The wording is partially stale.

Candidate action:

- Decide whether `git drs fetch` should be kept as an explicit LFS compatibility command, or deprecated in favor of `git drs pull` + `git drs query` workflows.
- Update command docs/help text to avoid implying `git lfs pull` is always used in `git drs pull`.

## 6.2 Duplicated logic

- Download option constants are duplicated:
  - `internal/drsremote/remote.go` (`DownloadResolvedToPath` via `downloadResolved`)
  - `cmd/download/main.go` (same `MultipartThreshold`, `Concurrency`, `ChunkSize` values).
- Branch/ref parsing logic is duplicated in `cmd/prepush/main.go`:
  - `readPushedRefs(...)` + `branchesFromRefs(...)`
  - `readPushedBranches(...)` (older equivalent helper)

Candidate action:

- Centralize transfer defaults into one constant/config source.
- Keep one branch parsing path in pre-push and remove the duplicate helper after test updates.

## 6.3 Low-value / effectively unused runtime code paths

- `readPushedBranches(...)` in `cmd/prepush/main.go` is not used by production pre-push flow (the flow uses `readPushedRefs` + `branchesFromRefs`), and appears retained mainly for tests (`cmd/prepush/main_test.go`).

Candidate action:

- Replace test usage with `readPushedRefs` + `branchesFromRefs`, then remove `readPushedBranches`.

## 6.4 Documentation-level deprecation candidates

- Any docs that describe `git drs fetch` as metadata-only should be revalidated against implementation (`cmd/fetch/main.go` currently performs `git lfs pull`).
- Any docs that describe `git drs pull` as an unconditional "git lfs pull" wrapper should be updated to reflect the DRS-driven download path.

---

## 7) `remote add gen3`: INI key persistence review

This section documents how `git drs remote add gen3` persists credentials into `~/.gen3/gen3_client_config.ini` and where current behavior is incorrect.

## 7.1 Current implementation path

- Command path: `cmd/remote/add/gen3.go` (`Gen3Cmd` -> `gen3Init(...)`).
- Credentials are persisted via `configure.Save(cred)` where `configure` is `github.com/calypr/syfon/client/config` manager.
- INI keys used by syfon config manager are:
  - `key_id`
  - `api_key`
  - `access_token`
  - `api_endpoint`

## 7.2 What gets written for each auth mode

### A) `--cred <json>` mode

`gen3Init(...)` imports a credentials file and sets:

- `cred.APIKey` -> saved as `api_key`
- `cred.KeyID` -> saved as `key_id`
- `cred.AccessToken` -> saved as `access_token`

This is generally correct for durable profile auth and token refresh behavior.

### B) `--token <token>` mode

`gen3Init(...)` sets only:

- `AccessToken` (saved as `access_token`)
- `APIKey` is empty
- `KeyID` is empty

Expected intent: profile should behave as token-only (no stale `api_key`/`key_id`).

## 7.3 Confirmed bug: stale INI keys are not cleared

`syfon/client/config.Manager.Save(...)` only writes keys when non-empty and does not clear existing keys when a field is empty.

Result:

- If a profile previously had `api_key`/`key_id` (from `--cred`) and is later updated with `--token`, old `api_key`/`key_id` remain in the INI section.
- This can leave the profile in a mixed state (new `access_token`, stale `api_key`/`key_id`).

Impact:

- Behavior may be confusing and can trigger unintended refresh attempts/validation paths that consider stale `api_key`.
- Operators may think they switched to token-only auth, but persisted profile still carries old key material.

## 7.4 Secondary `add remote` bug (related)

- `--url` is declared for `git drs remote add gen3` (`cmd/remote/add/init.go`) but is not used in `gen3Init(...)`.
- `gen3Init(...)` always derives `api_endpoint` from token/profile data.

This is not an INI key-name bug, but it is an `add remote` correctness bug that affects what endpoint is saved to INI.

## 7.5 Potential fixes

## Fix option 1 (preferred): make Save clear absent keys

In `syfon/client/config.Manager.Save(...)`:

- If `APIKey == ""`, delete `api_key` from section.
- If `KeyID == ""`, delete `key_id` from section.
- If `AccessToken == ""`, delete `access_token` from section.

This makes INI persistence reflect the caller's credential intent and prevents stale key carry-over.

## Fix option 2: explicit cleanup in `gen3Init(...)`

Before `configure.Save(cred)` in token-mode:

- Load existing profile
- explicitly blank/remove `api_key` and `key_id`
- then save

This localizes fix to git-drs but duplicates config semantics that are better handled in syfon manager.

## Fix option 3: wire `--url` and endpoint precedence

Update `gen3Init(...)` signature and call site to accept endpoint flag and use precedence:

1. explicit `--url` (if provided)
2. parse from provided auth token/cred
3. existing profile fallback

This resolves endpoint persistence drift and matches CLI expectation.

## 7.6 Suggested regression tests

- Start with profile containing `api_key` + `key_id` + `access_token`.
- Run `git drs remote add gen3 ... --token ...`.
- Assert INI section no longer contains stale `api_key`/`key_id` (or contains explicit empty values by chosen policy).
- Assert `api_endpoint` honors `--url` when provided.

