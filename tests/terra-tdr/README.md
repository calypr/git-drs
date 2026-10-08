# TDR pull integration

`run.sh` starts TDR's real DRS controller, generated API routes, and `DrsService`, then runs the `git-drs pull` integration test against that server. TDR stores the compact DRS ID mapping and the snapshot, source, and project records in an ephemeral PostgreSQL Testcontainer. The real `SnapshotService` and `SnapshotDao` load that metadata; the DAO's dataset materialization and empty table/relationship collaborators remain fixture-backed. TDR reads the seeded file record through its real `FileService` and `FireStoreDao` against the Firestore emulator. A GCS-compatible emulator serves the fixture object over HTTP.

Authorization remains fixture-backed. TDR uses the Google Cloud Storage Java client to look up the bucket and create a V4 signed URL with a generated test-only service-account key. A local token endpoint supplies its access token; no Google credentials are used. The test proxy changes only the signed URL's authority to point at the local GCS emulator. The emulator serves the bytes but does not validate the signature or expiry.

The pull starts with an empty cache, checks the hydrated bytes and TDR-provided checksums, and the runner asserts that the emulator served exactly one object download.

Run locally with Java 17, Go, and Docker installed:

```bash
tests/terra-tdr/run.sh /path/to/jade-data-repo
```

The CI workflow pins the TDR source revision. The test covers TDR's controller and `DrsService`, PostgreSQL DRS and snapshot lookup, Firestore file lookup, GCS client calls, the returned signed URL shape, and the CLI download path. It does not exercise Sam authorization or TDR's full `DatasetDao` materialization. The local token endpoint also does not validate Google-issued credentials.
