# Terra pull through DRS Hub and TDR

The `Resolve through DRS Hub and TDR` CI job starts pinned public TDR and DRS Hub source revisions and runs the real `git-drs` CLI binary against them. The fixture has three DRS objects: the original test ID and two IDs used by the public [AnVIL GREGoR reference repository](https://github.com/EllrottLab/AnVIL_GREGoR_R05_GRU). All payloads are tiny synthetic text, with truthful fixture sizes and checksums; this test does not download controlled genomic data. Hub expands `drs.anv0`, selects its Terra provider, and calls TDR's DRS API. TDR's controller and `DrsService` resolve the PostgreSQL DRS-to-snapshot mapping, snapshot metadata, and Firestore file metadata, then return Google Cloud Storage access URLs.

The test uses a generated service-account ADC JSON key and a local OAuth token endpoint. The token endpoint validates the signed JWT grant before returning a fixture bearer token. Hub's initial unauthenticated `OPTIONS` probe reaches TDR, followed by authenticated metadata and access requests. The test checks the request methods, statuses, and bearer token. TDR authorization and some snapshot/provider dependencies remain fixture-backed; Google credentials and external Sam are not needed.

TDR's real Google Storage client generates a V4 RSA signed URL using a generated test key. Before the CLI receives the Hub response, a local HTTPS bridge verifies the returned metadata and validates the signed URL's canonical request, RSA signature, and expiry. The bridge then changes only the signed URL authority so the CLI can reach the local GCS-compatible emulator from its Linux container. The emulator serves the fixture bytes but does not validate signed URLs itself.

The runner builds a Linux `git-drs` binary for the host architecture and runs it inside a pinned multi-architecture Alpine Git image. It configures a Terra remote with the CLI, checks `remote list` and `ping` against TDR's real service-info endpoint, selectively pulls one GREGoR ID, pulls the remaining pointers, repeats the pull from cache, and checks out a pointer through Git's process filter. It creates pointers with both `add-ref --manifest` and single-URI `add-ref`. Finally, a fixture token rejected by TDR must produce a clear authorization error without hydrating the file or writing the cache. The test checks the worktree, index, cache, metadata and per-object HTTP requests throughout, and asserts exactly one emulator download for each of the three objects.

This job covers reference authoring, successful hydration, and a TDR rejection of a fixture token. It does not exercise production Google/Sam authorization, a real GCS bucket, large transfers, or interrupted resume. Terra remotes are read-only; uploading and deleting Terra objects are not supported operations to test here.

Run locally using the pinned TDR and DRS Hub revisions in `.github/workflows/pr-checks.yaml`:

```sh
tests/terra-drs-hub/run.sh /path/to/jade-data-repo /path/to/terra-drs-hub
```
