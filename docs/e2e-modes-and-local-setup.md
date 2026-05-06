# E2E Modes And Local Setup

This guide documents how `git-drs` test suites run in:

- `remote` mode (Gen3 token auth)
- `local` mode (local drs-server, optional HTTP basic auth)

It also covers required drs-server configuration, common env vars, and expected behavior/logs.

## Env-first execution model

All six git-drs e2e scripts are designed to run from env vars (typically via `git-drs/.env`):

- `tests/e2e-gen3-remote-full.sh`
- `tests/e2e-gen3-remote-addurl.sh`
- `tests/e2e-local-full.sh`
- `tests/e2e-local-addurl.sh`
- `tests/monorepos/e2e-monorepo-remote.sh`
- `tests/monorepos/e2e-monorepo-local.sh`

Precedence:

- inline shell vars win (for example `TEST_BUCKET=... bash tests/...`)
- then `.env` values
- then script defaults

Default env file:

- `git-drs/.env`

Override:

```bash
ENV_FILE=/path/to/.env bash tests/e2e-local-full.sh
```

Realistic `.env` template (local mode + optional remote/GitHub paths):

```bash
TEST_SERVER_MODE=local
TEST_DRS_URL=http://localhost:8080

TEST_ORGANIZATION=<organization>
TEST_PROJECT_ID=<project_id>
TEST_BUCKET=<bucket>
TEST_BUCKET_NAME=<bucket>
TEST_BUCKET_REGION=<region>
TEST_BUCKET_ACCESS_KEY=
TEST_BUCKET_SECRET_KEY=
TEST_BUCKET_ENDPOINT=

TEST_CREATE_BUCKET_BEFORE_TEST=true
TEST_DELETE_BUCKET_AFTER=false

DRS_BASIC_AUTH_USER=<username>
DRS_BASIC_AUTH_PASSWORD=<password>

GEN3_PROFILE=<profile_name>
TEST_GITHUB_TOKEN=
TEST_COLLAB_USER_EMAIL=

TEST_PRINT_TOKEN=true
TEST_CLEANUP_RECORDS=true
TEST_STRICT_CLEANUP=true
```

## Concepts

### Remote mode

- `TEST_SERVER_MODE=remote`
- `git-drs` authenticates to DRS/index/data endpoints using bearer token (`TEST_GEN3_TOKEN` or `GEN3_PROFILE`)
- Common target: hosted Gen3-compatible deployment

### Local mode

- `TEST_SERVER_MODE=local`
- `git-drs` talks to local drs-server URL (`TEST_DRS_URL`, usually `http://localhost:8080`)
- Auth can be:
  - none (server local mode without basic auth), or
  - HTTP basic auth via:
    - `TEST_LOCAL_USERNAME` + `TEST_LOCAL_PASSWORD`, or
    - `TEST_ADMIN_AUTH_HEADER="Authorization: Basic <base64(user:pass)>"`
- `git drs remote add local ... --username ... --password ...` stores local basic auth in repo config for credential-helper flows.

## How wrapper scripts map to the main suites

`git-drs` local scripts are wrappers over the shared suites:

- `tests/e2e-local-full.sh` -> `tests/e2e-gen3-remote-full.sh`
- `tests/e2e-local-addurl.sh` -> `tests/e2e-gen3-remote-addurl.sh`
- `tests/monorepos/e2e-monorepo-local.sh` -> `tests/monorepos/e2e-monorepo-remote.sh`

Wrappers set `TEST_SERVER_MODE=local` and local defaults (`http://localhost:8080`, local org/project defaults, local auth env passthrough).

## drs-server configuration required for local mode

Minimum working config pattern:

```yaml
port: 8080
auth:
  mode: local
  basic:
    username: "drs-user" # example local test creds
    password: "drs-pass"
database:
  sqlite:
    file: "drs.db"
s3_credentials:
  - bucket: "<bucket_name>"
    region: "<region>"
    access_key: "<user>"
    secret_key: "<secret>"
    endpoint: "<endpoint>"
```

Notes:

- `auth.mode` is required and must be `local` or `gen3`.
- If you configure `auth.basic`, both username and password must be set.
- Local e2e tests that upload files require bucket credentials to exist for `TEST_BUCKET`.
- Env overrides are supported on server side:
  - `DRS_AUTH_MODE`
  - `DRS_BASIC_AUTH_USER`
  - `DRS_BASIC_AUTH_PASSWORD`
  - `DRS_DB_SQLITE_FILE`

## Local full E2E: runbook

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/e2e-local-full.sh
```

What it covers:

- `git drs push` metadata register + upload
- multipart/resume behavior
- `git drs pull` download and compatibility checks
- cleanup by DID resolution

## Local add-url E2E: runbook

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/e2e-local-addurl.sh
```

What it covers:

- known-sha add-url path (`--sha256 <real hash>`)
- unknown-sha add-url path (sentinel pointer OID)
- push/register + pull hydration checks

## Monorepo E2E (remote and local)

Remote:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_SERVER_MODE=remote \
GEN3_PROFILE="<profile_name>" \
TEST_DRS_URL="https://<drs-host>" \
TEST_ORGANIZATION="<organization>" \
TEST_PROJECT_ID="<project_id>" \
TEST_BUCKET="<bucket>" \
bash tests/monorepos/e2e-monorepo-remote.sh
```

Local:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_DRS_URL="http://localhost:8080" \
TEST_ORGANIZATION="<organization>" \
TEST_PROJECT_ID="<project_id>" \
TEST_BUCKET="<bucket>" \
TEST_LOCAL_USERNAME="<username>" \
TEST_LOCAL_PASSWORD="<password>" \
bash tests/monorepos/e2e-monorepo-local.sh
```

Parallel upload workers:

- `MONO_TRANSFERS` controls transfer concurrency.
- Alias supported: `TEST_PARALLEL_WORKERS` (used when `MONO_TRANSFERS` is unset).

Example:

```bash
TEST_PARALLEL_WORKERS=24 bash tests/monorepos/e2e-monorepo-local.sh
```

## Bucket provisioning in tests

If bucket credentials are already configured on the server, keep:

- `TEST_CREATE_BUCKET_BEFORE_TEST=false` (default for local wrappers)

If not configured, enable create-before-test:

```bash
TEST_CREATE_BUCKET_BEFORE_TEST=true \
TEST_BUCKET_NAME="<bucket_name>" \
TEST_BUCKET_REGION="<region>" \
TEST_BUCKET_ACCESS_KEY="<user>" \
TEST_BUCKET_SECRET_KEY="<secret>" \
TEST_BUCKET_ENDPOINT="<endpoint>" \
TEST_DELETE_BUCKET_AFTER=true \
bash tests/e2e-local-full.sh
```

## Troubleshooting patterns

### `APIKey is required to refresh access token` in local mode

Cause:

- local run accidentally using remote/gen3 auth path.

Check:

- `TEST_SERVER_MODE=local`
- remote configured with `git drs remote add local ...`
- local auth creds provided if server requires basic auth.

### `401 Unauthorized` on `/data/upload/...` in local mode

Cause:

- missing/incorrect basic auth.

Fix:

- set `TEST_LOCAL_USERNAME`/`TEST_LOCAL_PASSWORD`
- or set `TEST_ADMIN_AUTH_HEADER`.

### `credential not found` or `bucket credential not found`

Cause:

- server has no bucket mapping/credential for `TEST_BUCKET`.

Fix:

- preconfigure bucket credentials on server, or
- run tests with `TEST_CREATE_BUCKET_BEFORE_TEST=true` and valid bucket env.

### Resumable multipart test expected failure did not fail

If a run says:

- `error: expected first resumable multipart upload attempt to fail`

then multipart path likely did not trigger. Ensure:

- multipart threshold env values are lower than the target file size, and
- latest `git-drs` binary is installed (`go install ./...`).

## Local auth env convenience

If server local mode uses basic auth, these are supported:

```bash
DRS_BASIC_AUTH_USER=<username>
DRS_BASIC_AUTH_PASSWORD=<password>
TEST_LOCAL_USERNAME=${DRS_BASIC_AUTH_USER}
TEST_LOCAL_PASSWORD=${DRS_BASIC_AUTH_PASSWORD}
```
