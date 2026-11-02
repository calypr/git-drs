# Terra/AnVIL TDD acceptance tests

## Purpose

This document explains the test-driven development approach for the Terra/AnVIL DRS work and how to run the tests that currently describe the desired behavior.

The tests are intentionally written before the production implementation. They are expected to fail until `git-drs` supports:

- a `terra` remote type;
- Terra-aware DRS reference creation;
- DRS URI-shaped `git-drs` pointer files;
- deterministic SHA256-shaped local cache identity for DRS URI references.

These tests turn the ADR acceptance criteria into executable checks so implementation can proceed incrementally.

## Test files

### `internal/config/terra_remote_acceptance_test.go`

Covers the Terra remote configuration acceptance criterion.

The test writes Git config entries equivalent to a future Terra remote:

```ini
[drs]
  default-remote = anvil
[drs "remote.anvil"]
  type = terra
  endpoint = https://data.terra.bio
  auth = google-adc
  mode = read-only
```

Expected future behavior:

- `LoadConfig` recognizes `type = terra`.
- `Config.GetRemote("anvil")` returns a non-nil DRS remote.
- The remote preserves the configured Terra DRS endpoint.

Current behavior:

- The test fails because config parsing only models existing Gen3/local remotes.

### `cmd/addref/terra_addref_acceptance_test.go`

Covers the remote-aware reference creation acceptance criterion.

Expected future behavior:

- `git drs add-ref` exposes a `--remote-type` flag or equivalent resolver-selection mechanism.
- Terra DRS references can select Terra resolver behavior explicitly when creating pointer metadata.

Current behavior:

- The test fails because `add-ref` does not expose `--remote-type`.

### `cmd/ping/main_test.go`

Covers the Terra remote connectivity acceptance criterion for `git drs ping`.

The acceptance test configures a future Terra remote named `anvil` and uses an
`httptest` server to stand in for the Terra/AnVIL DRS endpoint:

```ini
[drs]
  default-remote = anvil
[drs "remote.anvil"]
  type = terra
  endpoint = http://127.0.0.1:<test-port>
  auth = google-adc
  mode = read-only
```

Expected future behavior:

- `git drs ping anvil` recognizes the configured `terra` remote.
- The command prints Terra remote status, including `type: terra` and the
  configured endpoint.
- The command pings the Terra/AnVIL DRS service-info route at
  `/ga4gh/drs/v1/service-info`.
- A successful service-info response is reported as `health: ok`.

Current behavior:

- The test passes once runtime construction recognizes Terra remotes and
  `git drs ping` checks the configured DRS service-info endpoint.
- This test should remain a regression test for Terra/AnVIL ping connectivity.

### `internal/lfs/terra_pointer_acceptance_test.go`

Covers the DRS URI pointer compatibility and cache identity acceptance criteria.

Expected future pointer behavior:

```text
version https://calypr.github.io/spec/v1
oid drs://cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c
size 11305017366
```

Expected future cache behavior:

- A DRS URI pointer can still be mapped to a deterministic SHA256-shaped local cache key.
- The cache key may be derived from a normalized DRS URI rather than from payload content.
- Existing cache fanout assumptions can remain SHA256-shaped even when the pointer `oid` is a DRS URI.

Current behavior:

- The pointer parser rejects non-`sha256` OID types.
- `ObjectPath` rejects raw DRS URIs because it currently requires a 64-character SHA256 hex string.

## How to run the focused tests

From the repository root, run:

```bash
go test -timeout 30s ./internal/lfs ./internal/config ./cmd/addref ./cmd/ping
```

These tests are currently expected to fail. A failure is useful because it confirms the tests are exercising missing Terra/AnVIL behavior rather than silently passing against existing Gen3/Syfon-only behavior.

## Expected current output shape

The exact timing may differ, but the failures should include messages like:

```text
--- FAIL: TestAcceptanceTerraDRSURIoidPointerIsRecognized
    terra_pointer_acceptance_test.go: expected git-drs DRS URI pointer to be recognized

--- FAIL: TestAcceptanceTerraDRSURIoidPointerHasDeterministicSHA256CacheKey
    terra_pointer_acceptance_test.go: expected DRS URI to be normalized to a sha256-shaped cache key path

--- FAIL: TestAcceptanceLoadTerraRemoteConfig
    terra_remote_acceptance_test.go: expected terra remote to load as a DRS remote

--- FAIL: TestAcceptanceAddRefExposesRemoteTypeForTerraReferences
    terra_addref_acceptance_test.go: expected add-ref to expose --remote-type

```

## How to run individual test groups

### Pointer and cache identity tests

```bash
go test -timeout 30s ./internal/lfs -run 'TestAcceptanceTerraDRSURIoidPointer'
```

Use this while implementing pointer parsing, DRS URI normalization, and cache-key derivation.

### Terra remote config test

```bash
go test -timeout 30s ./internal/config -run TestAcceptanceLoadTerraRemoteConfig
```

Use this while adding config structs, parsing, and persistence for `type = terra` remotes.

### Terra-aware `add-ref` test

```bash
go test -timeout 30s ./cmd/addref -run TestAcceptanceAddRefExposesRemoteTypeForTerraReferences
```

Use this while adding CLI surface for remote-aware Terra reference creation.

### Terra-aware `ping` test

```bash
go test -timeout 30s ./cmd/ping -run TestAcceptancePingTerraDRSServer
```

Use this while wiring Terra remotes into runtime construction and adding the
Terra/AnVIL DRS service-info health probe used by `git drs ping`.

## Recommended implementation order

1. Add a Terra remote model to config parsing and persistence.
2. Add a Terra-aware resolver abstraction that can resolve DRS URIs through the configured remote.
3. Add a Terra-aware ping path that checks the configured DRS service-info endpoint.
4. Extend `add-ref` so it can create references using a selected remote or remote type.
5. Decide the pointer-format strategy:
   - keep Git LFS-shaped `oid sha256:<derived-local-oid>` pointers with DRS URI in metadata; or
   - support `version https://calypr.github.io/spec/v1` with `oid drs://...` directly.
6. Add deterministic DRS URI to SHA256-shaped cache-key derivation.
7. Add manifest bootstrap/import as a batch wrapper over the reference creation primitive.

## When the tests should pass

The focused tests should pass when `git-drs` can:

- load a read-only Terra remote from Git config;
- ping the configured Terra/AnVIL DRS service-info endpoint;
- expose a Terra resolver selection path for `add-ref`;
- parse DRS URI-shaped `git-drs` pointer files;
- map DRS URI identities to deterministic SHA256-shaped cache paths.

At that point, the failing TDD tests should be treated as regression tests for the first Terra/AnVIL support increment.
