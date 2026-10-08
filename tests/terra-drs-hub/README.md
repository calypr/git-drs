# DRS Hub to TDR integration

The `Resolve through DRS Hub and TDR` CI job runs the pinned public DRS Hub service and sends its real `POST /api/v4/drs/resolve` endpoint an AnVIL compact DRS ID. Hub expands `drs.anv0`, chooses its TDR provider, and makes the GA4GH DRS `OPTIONS`, object metadata, and access URL calls. Those HTTPS requests pass through a local TLS forwarder to the running TDR harness. The forwarder only translates HTTPS to the harness's local HTTP listener; it does not generate DRS responses.

The TDR harness runs the actual DRS controller and `DrsService`. The snapshot, authorization, file record, and URL signing boundaries use test fixtures. The request uses the fixed test bearer token `tdr-ci-fixture`; authenticated metadata and access requests forward that token to TDR. Hub's DRS `OPTIONS` probe omits the bearer token, so the fixture rejects that probe and Hub exercises its configured-auth fallback before making the authenticated `GET` calls. The signed URL points to the harness's local fixture bytes, so the test needs no Google credentials or bucket access.

This check covers Hub's AnVIL routing and TDR DRS client behavior separately from the `git-drs pull` integration check. `git-drs` currently resolves directly against TDR, so its pull test does not pass through Hub.

To run locally, check out the pinned TDR and DRS Hub revisions used in `.github/workflows/pr-checks.yaml`, then run:

```sh
tests/terra-drs-hub/run.sh /path/to/jade-data-repo /path/to/terra-drs-hub
```
