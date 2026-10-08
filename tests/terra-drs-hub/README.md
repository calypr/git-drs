# Terra pull through DRS Hub and TDR

The `Resolve through DRS Hub and TDR` CI job runs a single `git-drs pull` against an AnVIL pointer. The CLI sends the DRS ID to a locally running DRS Hub. Hub expands `drs.anv0`, selects its TDR provider, and calls TDR's DRS API. TDR resolves the object through its real DRS controller, `DrsService`, PostgreSQL DRS ID and snapshot records, and Firestore file record. The CLI downloads the returned bytes and hydrates the worktree file. The test checks the hydrated bytes, a Hub resolve call, and authenticated TDR metadata and access requests.

The external authorization boundary uses a fixed test bearer token (`tdr-ci-fixture`) instead of Google credentials or Sam. Hub's unauthenticated DRS `OPTIONS` probe is rejected by that boundary, exercising Hub's configured-auth fallback. The local TLS proxy forwards Hub's requests to TDR and records them; it does not fabricate DRS responses. TDR's real Google Cloud Storage client looks up the seeded bucket and signs the access URL with a generated test-only key. A local token endpoint supplies an access token, and the GCS-compatible emulator serves the object bytes. The CLI-side test bridge changes the signed URL authority to the emulator's local address. The emulator does not validate signed URL signatures or expiry. The runner asserts one GCS object download.

Run locally using the pinned TDR and DRS Hub revisions in `.github/workflows/pr-checks.yaml`:

```sh
tests/terra-drs-hub/run.sh /path/to/jade-data-repo /path/to/terra-drs-hub
```
