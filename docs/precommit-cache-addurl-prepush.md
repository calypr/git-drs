# ADR 0002: Align add-url and pre-push with the pre-commit cache

## Status
Proposed

## Context
`cmd/precommit` now maintains a local cache under `.git/drs/pre-commit/v1` that records:
- path → LFS OID in `paths/<encoded-path>.json`
- OID → paths + URL hint in `oids/<oid-hash>.json`

`precommit_cache` provides read helpers for this cache and is intended to let the pre-push hook validate against authoritative sources while using cached hints to avoid re-scanning worktrees. `cmd/addurl` currently writes the LFS pointer and DRS files but does not update the pre-commit cache. `cmd/prepush` currently computes updates without consulting the cache. This means:
- `add-url`-created objects are invisible to cache-aware workflows unless a pre-commit hook runs later.
- `pre-push` cannot leverage cached OID/path/url hints or detect mismatches early.

## Decision
Update `cmd/addurl` and `cmd/prepush` to integrate with the pre-commit cache, while preserving the current fallback behavior when the cache is missing or stale.

### Changes required in `cmd/addurl`
1. **Write cache entries after LFS pointer creation**
   - Create/update the path entry (`paths/<encoded-path>.json`) using the same encoding as `cmd/precommit` (`base64.RawURLEncoding` of the repo-relative path).
   - Create/update the OID entry (`oids/<oid-hash>.json`) using the same OID hashing (`sha256(oid string)`), ensuring the `paths` list includes the new path.
2. **Persist the external URL hint**
   - Record the supplied S3 URL in the OID entry as the URL hint for pre-push.
   - Align the JSON field name with the cache reader expectations (currently `external_url` in `precommit_cache`).
3. **Respect cache semantics**
   - Preserve existing `paths` for the same OID, append new ones idempotently, and update `updated_at`.
   - Set `content_changed` when the path previously existed with a different OID.
4. **Graceful fallback**
   - If the cache directory is missing or non-writable, log a warning and continue without failing the `add-url` command.

### Changes required in `cmd/prepush`
1. **Use `precommit_cache` to seed work**
   - Open the cache early and, when available, use it to map pushed paths/branches to their LFS OIDs and cached URL hints.
   - If the cache is missing or entries are stale, fall back to current discovery/update logic.
2. **Validate cached URL hints**
   - When `updateDrsObjects` resolves authoritative URLs, compare them to cached hints via `precommit_cache.CheckExternalURLMismatch`.
   - Warn (or fail, depending on policy) on mismatches to surface potentially stale or incorrect metadata before pushing.
3. **Prefer cache data for DRS updates**
   - Use cached OIDs/paths to reduce redundant file scans for LFS pointers.
   - Carry cached `external_url` into DRS metadata when authoritative sources are unavailable, while still treating it as non-authoritative.

## Consequences
- `add-url` updates the same local cache used by pre-commit, so pre-push sees consistent data even when no commit occurs between add and push.
- `pre-push` can use cached hints to speed up DRS updates and detect mismatches earlier, with a clear fallback path when cache data is missing or stale.
- Cache schema alignment (`external_url` naming and OID hashing) becomes a shared contract between `cmd/precommit`, `cmd/addurl`, and `precommit_cache`.
