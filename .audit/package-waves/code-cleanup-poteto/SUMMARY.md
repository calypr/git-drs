# Poteto Go cleanup audit

Commit `3388d6c8` removed the largest duplicate and dead paths. A second behavior-preserving wave can remove about 65 to 75 production lines and reduce two package-level mutable test seams.

## Implement next

1. Pass the selected bucket into `gen3Init`. Remove temporary mutation of `fenceToken` and `selectedBucket` from the embedded `ConfigureGen3` path.
2. Store the multipart backend on `pushRuntime`. Inline `needsUpload` and `hasLocalPayload`. Replace the private-helper test with candidate or summary behavior.
3. Reuse `getExactBucketMapping` for the project lookup in `GetBucketMapping`.
4. Inline the two filter passthrough response wrappers.
5. Remove unread command state from `precommit.Change`, `syncRefState`, and `cmd/rm`. Remove the unused context from `handleDelete`.
6. Delete exact one-caller wrappers in add-ref, add-url, and pull. Call `lfs.CreateDRSPointer` and `lfs.IsDRSURI` directly.
7. Remove the unused private LFS logger parameter and the unreachable precommit-cache not-found branch.
8. Test path traversal through public `Client.Pull` instead of private `safeRelativePath`.

Run focused package tests after each item. Run the full repository suite once after integration.

## Require contracts first

- Define bucket-selection policy with a table before sharing project fallback, organization fallback, preferred bucket, ambiguity, and ping behavior.
- Characterize deterministic DRS IDs before hiding the UUID namespace behind one function.
- Prove dependency isolation before replacing `copyrecords` package globals.
- Characterize cache mutation, add-url source schemes, mixed-case inventory OIDs, and command phase ordering before changing those shapes.

## Keep

- Keep the public `client` and `inventory` packages.
- Keep `config.DRSRemote`, `cmd/copyrecords.indexAPI`, and `resolver.NewAnVILWithClient`.
- Keep transfer access, metadata, Globus, cache, and progress policy that Syfon v0.3.8 does not implement.
- Do not split the large pull or copy-records commands without command-level phase tests.

## Verification

`.audit/package-waves/code-cleanup-poteto/analyze.sh` runs `go vet`, dead-code analysis, and targeted scans for mutable package state and ignored errors with isolated Go caches. The script passes. The installed `staticcheck` and `golangci-lint` binaries do not support this repository's Go 1.25 code, so their type-check failures are not findings.

## Implementation result

The wave reduced production Go from 16,907 to 16,833 physical lines and from 14,942 to 14,884 code lines. That is 74 fewer physical lines and 58 fewer executable lines. The implementation removed the transfer and Gen3 package-global mutation, exact one-caller wrappers, unread command state, and unreachable branches described above.

Verification after integration:

- `go test ./... -count=1`: 420 tests passed in 48 packages.
- `go vet ./...`: no findings.
- Dead-code analysis reports only intentional public entry points and test helpers.
