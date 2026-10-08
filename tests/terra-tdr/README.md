# TDR controller integration

`run.sh` copies `TerraDrsHarnessTest.java` into a checkout of the pinned TDR source, starts its real DRS controller, generated API routes, and `DrsService`, then runs the git-drs pull integration test against that server. The test provides fixture snapshot and file metadata and mocks authorization and Google URL signing. It checks that git-drs fetches the fixture bytes from an empty cache and verifies the TDR-produced CRC32C and MD5 checksums. It needs no Terra credentials or cloud storage.

Run locally with Java 17 and Go installed:

```bash
tests/terra-tdr/run.sh /path/to/jade-data-repo
```

The CI job pins the TDR source commit in `.github/workflows/pr-checks.yaml`. This test covers TDR's DRS object and access URL construction and the git-drs pull path. It does not exercise TDR's database, Sam authorization, or cloud storage integrations.
