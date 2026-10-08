# TDR controller integration

`run.sh` copies `TerraDrsHarnessTest.java` into a checkout of the pinned TDR source, starts its real DRS controller and generated API routes, then runs the git-drs pull integration test against that server. The harness mocks TDR authentication and `DrsService`; it serves a small file with TDR-style CRC32C and MD5 checksums. It needs no Terra credentials or cloud storage.

Run locally with Java 17 and Go installed:

```bash
tests/terra-tdr/run.sh /path/to/jade-data-repo
```

The CI job pins the TDR source commit in `.github/workflows/pr-checks.yaml`. This test covers the TDR HTTP controller contract and the git-drs pull path. It does not exercise TDR's database, Sam authorization, or cloud storage integrations.
