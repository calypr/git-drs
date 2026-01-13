
# Tests for `client/indexd/tests`

Overview
- This directory contains unit and integration tests related to the indexd client.
- Tests may exercise network services (indexd, Postgres, GitHub) and can be destructive for remote resources. Run in an isolated/test environment.

Location
- `client/indexd/tests`

Prerequisites
- macOS development environment.
- Go toolchain (Go modules enabled). Recommended: Go 1.20+.
- Ensure required binaries are on `PATH` if tests call external tools (for example: `git`, `git-lfs`, `docker`).
- Run `go mod download` from the repository root before running tests.

Common environment variables
- `GH_PAT` — GitHub token (used by tests that create/delete repos).
- `GIT_DRS_REMOTE` — profile/remote name used by git-drs tests.
- `INDEXD_URL` / `INDEXD_TOKEN` — endpoint and token if tests interact with an indexd service.
- `POSTGRES_HOST`, `POSTGRES_PASSWORD` — if tests need a Postgres instance.
Note: Not all variables are required for every test. Check individual test code for exact requirements.

Running tests
- From repository root:
  - `go test ./client/indexd/tests -v`
- From the tests directory:
  - `cd client/indexd/tests && go test -v`
- Run a single test (by name or regex):
  - `go test -v -run TestName`
- Force rebuild / avoid caching:
  - `go test -v -run TestName -count=1`
- Run with race detector:
  - `go test -race -v`
- Increase test timeout:
  - `go test -v -timeout 5m`

Notes & troubleshooting
- If tests fail due to missing services, start required services (indexd, Postgres, etc.) or mock them as appropriate.
- If tests modify remote resources (GitHub repos, buckets), provide credentials pointing to a disposable/test account/org.
- Inspect test logs (`-v`) for external command output.
- If a test depends on `git-lfs`, ensure `git lfs install --skip-smudge` has been run in the environment or CI image.

CI / Safety
- Run integration tests in controlled CI or a dedicated test environment.
- Use separate credentials and test orgs to avoid impacting production data.

