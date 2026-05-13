# Bug Doc: Checksum Match Can Suppress Required Upload

## Current behavior

`internal/pushsync` currently treats an existing checksum match as reusable without proving that the referenced payload is still downloadable. This is intentional for now because it avoids extra probe traffic during `git drs push`, but it weakens the older availability guarantee.

In practice, a stale metadata record, broken backing blob, or bad access URL can still cause push preparation to skip upload and proceed to `git push`, leaving refs that later cannot be pulled successfully.

## Why this is still a bug

The behavior is currently accepted as a tradeoff, but it is still a bug from a data-availability perspective because checksum metadata alone is not enough to prove that the payload bytes remain recoverable.

## Follow-up

- Decide whether to restore downloadability probes for same-scope matches and reusable cross-scope matches.
- If probes remain disabled, document the weaker availability guarantee in user-facing push and troubleshooting docs.
- Add a focused regression test once the long-term policy is chosen.
