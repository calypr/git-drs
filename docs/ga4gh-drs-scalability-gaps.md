# GA4GH DRS Scalability Gaps for `git-drs`

## Summary

GA4GH DRS is a reasonable read/access protocol for individual object resolution, but it is not a good standalone fit for high-volume `git-drs` workflows when the client must translate one logical operation into many REST calls.

The problem is not that DRS is incorrect. The problem is that the base API surface is too narrow for the operational patterns `git-drs` actually needs:

- checksum-first lookup
- bulk existence checks
- bulk access URL resolution
- bulk registration
- upload orchestration
- scoped delete/update flows

If every one of those has to be decomposed into multiple per-object HTTP calls, the result is not operationally scalable.

## The core issue

For `git-drs`, many user-facing operations are logically single operations:

- "do these 500 OIDs already exist?"
- "give me access URLs for these 200 objects"
- "register these 150 objects"
- "delete the objects that match this checksum in this scope"

Base GA4GH DRS mostly gives the client:

- `GET /ga4gh/drs/v1/objects/{object_id}`
- `GET /ga4gh/drs/v1/objects/{object_id}/access/{access_id}`
- `GET /ga4gh/drs/v1/service-info`

That works for one object at a time. It does not work well for batch-oriented Git and LFS workflows.

## Why this is not scalable

### 1. One logical operation turns into N network round-trips

Examples:

- checksum lookup by many OIDs:
  - no native bulk checksum query in base DRS
  - client must fan out one request per checksum
- access resolution for many objects:
  - no native bulk access URL resolution in base DRS
  - client must fan out one request per object/access method
- delete by hash:
  - client must first resolve matching objects
  - then issue one delete per object

This is acceptable for a handful of objects. It is poor for large repos, monorepos, or pre-push/pull batch flows.

### 2. Latency compounds even when payload size is small

Most of these calls are metadata calls, not large transfers. The bottleneck is round-trip count:

- HTTP connection overhead
- auth middleware and token processing
- authz lookup
- request routing
- JSON encode/decode
- repeated server-side DB lookups

A workflow that should feel like "one batch metadata operation" becomes "hundreds of tiny RPCs over HTTP".

### 3. Capability gaps force client-side orchestration complexity

Without batch primitives, the client has to own:

- fan-out scheduling
- retry policy
- partial failure handling
- de-duplication
- concurrency limits
- fallback behavior when some objects resolve and some do not

That complexity is not free. It moves real system cost from the server contract into every client.

### 4. It weakens read-path ergonomics for Git/LFS-shaped workflows

`git-drs` is not a generic browser for one object at a time. It is operating on working trees, pointer inventories, checksum sets, and batch synchronization.

Those workflows are naturally set-oriented, not object-at-a-time.

Trying to force them through only:

- `GetObject(id)`
- `GetAccessURL(id, access_id)`

produces a client that is formally "more DRS-pure" but operationally worse.

## Concrete examples

### Bulk checksum validity

Logical operation:

- "tell me which of these 1000 SHA256 values already exist"

Base DRS shape:

- 1000 repeated checksum/object lookups

What the client actually needs:

- one bulk validity response:
  - `sha256 -> exists`

This is why `/index/bulk/sha256/validity` or an equivalent bulk DRS extension exists.

### Bulk access resolution

Logical operation:

- "hydrate all unresolved pointer files in this checkout"

Base DRS shape:

- resolve object for each OID
- resolve access URL for each object

That means at least:

- one object-resolution request per object
- one access-resolution request per object

For a checkout with many files, this is an avoidable round-trip explosion.

### Delete by checksum or scoped cleanup

Logical operation:

- "delete matching records for this checksum in this repo scope"

Base DRS shape:

1. resolve records
2. iterate matching IDs
3. delete one by one

That is technically possible. It is not a good contract for batch cleanup.

## Position

`git-drs` should not be forced into a "base DRS only" model when that model degrades correctness, simplicity, or scale.

The better architecture is:

- use base GA4GH DRS where it fits naturally
  - single-object read
  - access resolution
  - service-info probing
- keep explicit extension APIs where batch or write semantics are required
  - bulk checksum validity
  - bulk checksum lookup
  - bulk access URL resolution
  - bulk registration
  - upload negotiation
  - batch delete/update helpers

## Recommended design rule

Do not collapse one logical client operation into multiple mandatory REST calls unless there is no practical alternative.

More concretely:

- if a client operation is inherently batch-oriented, prefer a batch endpoint
- if a workflow is write-oriented, do not pretend it is standard DRS when it is not
- if an optimization is required for acceptable repo-scale performance, keep it as an explicit extension instead of hiding it behind repeated single-object DRS calls

## Recommended client split

`git-drs` should continue to distinguish:

- **DRS read contract**
  - `GetObject`
  - `GetAccessURL`
  - `GetServiceInfo`

- **DRS extension contract**
  - bulk checksum lookup / validity
  - object registration
  - upload request negotiation
  - bulk access resolution
  - delete/update helpers

That is a more honest and more scalable model than pretending all useful client operations can be reduced to base DRS primitives without cost.

## Bottom line

GA4GH DRS is a solid object access protocol.

It is not, by itself, a scalable batch control-plane for `git-drs`.

Where one custom operation would otherwise require two or more mandatory REST operations per object, a dedicated extension is justified. The scalability cost is real, and the client should not absorb it just to preserve a cleaner-looking but weaker protocol story.
