# Terra pull through DRS Hub and TDR

The `Resolve through DRS Hub and TDR` CI job starts the pinned public TDR and DRS Hub source revisions and runs the real `git-drs` CLI binary against them. Hub expands the `drs.anv0` compact ID, selects its Terra provider, and calls TDR's DRS API. TDR's controller and `DrsService` resolve the PostgreSQL DRS-to-snapshot mapping, snapshot metadata, and Firestore file metadata, then return a Google Cloud Storage access URL.

The test uses a generated service-account ADC JSON key and a local OAuth token endpoint. The token endpoint validates the signed JWT grant before returning a fixture bearer token. Hub's initial unauthenticated `OPTIONS` probe reaches TDR, followed by authenticated metadata and access requests. The test checks the request methods, statuses, and bearer token. TDR authorization and some snapshot/provider dependencies remain fixture-backed; Google credentials and external Sam are not needed.

TDR's real Google Storage client generates a V4 RSA signed URL using a generated test key. Before the CLI receives the Hub response, a local HTTPS bridge verifies the returned metadata and validates the signed URL's canonical request, RSA signature, and expiry. The bridge then changes only the signed URL authority so the CLI can reach the local GCS-compatible emulator from its Linux container. The emulator serves the fixture bytes but does not validate signed URLs itself.

The runner builds a Linux `git-drs` binary for the host architecture and runs it inside a pinned multi-architecture Alpine Git image. It configures the actual clean, smudge, and process filters, checks that the worktree contains the downloaded bytes while the index still contains the pointer, verifies the cache, and runs a second pull to confirm it makes no additional Hub, OAuth, or TDR requests. It also asserts that the emulator served exactly one object download.

Run locally using the pinned TDR and DRS Hub revisions in `.github/workflows/pr-checks.yaml`:

```sh
tests/terra-drs-hub/run.sh /path/to/jade-data-repo /path/to/terra-drs-hub
```
