# Testing Git DRS

Developer test suites for `git-drs` (local and remote integration). These are intentionally heavier than CI smoke tests.

See also: [E2E Modes + Local Setup](../docs/e2e-modes-and-local-setup.md)

## Prerequisites

- `git`
- `git-lfs`
- `git-drs` (the binary you want to test)
- `jq`
- `curl`
- `shasum`

## Env-first workflow (all six tests)

All six e2e scripts in this directory are designed to run from environment variables loaded from `git-drs/.env` by default.

Variable precedence:

- inline shell vars win (for example `TEST_BUCKET=... bash tests/...`)
- then `.env` values
- then script defaults

Use a different env file:

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

## Test matrix

- `tests/e2e-gen3-remote-full.sh`
  - full push/pull workflow
  - multipart/resume checks
  - LFS compatibility path
- `tests/e2e-gen3-remote-addurl.sh`
  - add-url with known SHA and unknown SHA sentinel path
- `tests/e2e-local-full.sh`
  - wrapper for full suite in `local` server mode
- `tests/e2e-local-addurl.sh`
  - wrapper for add-url suite in `local` server mode
- `tests/monorepos/e2e-monorepo-remote.sh`
  - large fixture monorepo flow in remote mode
- `tests/monorepos/e2e-monorepo-local.sh`
  - monorepo local-mode wrapper

## Quick runs

### Remote full

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/e2e-gen3-remote-full.sh
```

### Local full

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/e2e-local-full.sh
```

### Local add-url

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/e2e-local-addurl.sh
```

### Monorepo remote

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

bash tests/monorepos/e2e-monorepo-remote.sh
```

### Monorepo local

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_PARALLEL_WORKERS=24 \
bash tests/monorepos/e2e-monorepo-local.sh
```

`TEST_PARALLEL_WORKERS` is accepted by monorepo suite as an alias for transfer concurrency (`MONO_TRANSFERS`).

## Auth and mode variables

- Remote mode:
  - `TEST_SERVER_MODE=remote` (default for remote scripts)
  - `TEST_GEN3_TOKEN` or `GEN3_PROFILE` / `TEST_GEN3_PROFILE`
- Local mode:
  - `TEST_SERVER_MODE=local` (set by local wrappers)
  - `TEST_LOCAL_USERNAME` + `TEST_LOCAL_PASSWORD`, or `TEST_ADMIN_AUTH_HEADER`

## Bucket provisioning variables

Use when bucket credentials are not already configured on server:

- `TEST_CREATE_BUCKET_BEFORE_TEST=true`
- `TEST_BUCKET_NAME`
- `TEST_BUCKET_REGION`
- `TEST_BUCKET_ACCESS_KEY`
- `TEST_BUCKET_SECRET_KEY`
- `TEST_BUCKET_ENDPOINT`
- optional: `TEST_BUCKET_ORGANIZATION`, `TEST_BUCKET_PROJECT_ID`
- `TEST_DELETE_BUCKET_AFTER=true|false`

If bucket credentials are preconfigured, keep `TEST_CREATE_BUCKET_BEFORE_TEST=false`.

## Debugging variables

- `TEST_KEEP_WORKDIR=true` keeps temp repo dirs for inspection
- `TEST_CLEANUP_RECORDS=true|false`
- `TEST_STRICT_CLEANUP=true|false`
- `TEST_DEBUG_AUTH=true|false`
- monorepo:
  - `MONO_RUN_CLONE_VERIFY=true|false`
  - `MONO_RUN_MULTIPART_SMOKE=true|false`

## Common failures and direct causes

- `APIKey is required to refresh access token`
  - local test is accidentally running remote auth path
- `401 Unauthorized` on `/data/upload/...`
  - missing/invalid local basic auth
- `bucket credential not found` or `credential not found`
  - server missing bucket credential mapping for `TEST_BUCKET`
- `expected first resumable multipart upload attempt to fail`
  - multipart path not triggered (threshold/binary mismatch)

## Coverage test script

Script: `tests/coverage-test.sh`

This is a broader environment-sensitive developer script and is heavier than standard e2e suites.
