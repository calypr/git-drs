# Testing Git DRS

This directory contains developer-run tests (local and remote). These are not designed for fast CI smoke checks.

## Prerequisites

- `git`
- `git-lfs`
- `git-drs`
- `jq`
- `curl`
- `shasum`

Most scripts auto-load env vars from `git-drs/.env` (or `ENV_FILE=/path/to/.env`).

## Main E2E Suites

### 1) Comprehensive remote flow

Script: `tests/e2e-gen3-remote-full.sh`

Covers:

- `git drs push` register/upload workflow
- resumable multipart upload/download behavior
- `git drs pull`
- stock `git lfs pull` compatibility check
- optional endpoint sweeps and optional GitHub-backed git remote

Minimal run:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_ORGANIZATION='calypr' \
TEST_PROJECT_ID='end_to_end_test' \
TEST_BUCKET='cbds' \
GEN3_PROFILE='my-profile' \
bash tests/e2e-gen3-remote-full.sh
```

### 2) Add-url remote flow (known + unknown SHA)

Script: `tests/e2e-gen3-remote-addurl.sh`

Covers both add-url paths in one run:

- known SHA path: `git drs add-url ... --sha256 <real_sha>`
- unknown SHA path: `git drs add-url ...` (synthetic/sentinel OID)
- push registration
- clone + `git drs pull` content verification
- DRS checksum lookups for both registered OIDs

Run:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_ORGANIZATION='calypr' \
TEST_PROJECT_ID='end_to_end_test' \
TEST_BUCKET='cbds' \
GEN3_PROFILE='my-profile' \
bash tests/e2e-gen3-remote-addurl.sh
```

## Local Mode E2E

Script: `tests/e2e-local-full.sh`

This is a wrapper around the remote-full suite configured for local drs-server mode.

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_DRS_URL='http://127.0.0.1:8080' \
TEST_ORGANIZATION='cbdsTest' \
TEST_PROJECT_ID='git_drs_e2e_test' \
TEST_BUCKET='cbds' \
bash tests/e2e-local-full.sh
```

## Common Environment Variables

Used by the main remote suites:

- `TEST_DRS_URL`
- `TEST_ORGANIZATION`
- `TEST_PROJECT_ID`
- `TEST_BUCKET`
- `TEST_GEN3_TOKEN` or `GEN3_PROFILE` / `TEST_GEN3_PROFILE`

Optional bucket bootstrap (create+delete bucket credential/scope during test):

- `TEST_CREATE_BUCKET_BEFORE_TEST=true`
- `TEST_BUCKET_NAME`
- `TEST_BUCKET_REGION`
- `TEST_BUCKET_ACCESS_KEY`
- `TEST_BUCKET_SECRET_KEY`
- `TEST_BUCKET_ENDPOINT`
- `TEST_BUCKET_ORGANIZATION` (optional)
- `TEST_BUCKET_PROJECT_ID` (optional)
- `TEST_DELETE_BUCKET_AFTER=true`

Debugging helpers:

- `TEST_KEEP_WORKDIR=true`
- `TEST_CMD_TIMEOUT_SECONDS`
- `TEST_HTTP_MAX_TIME`
- `TEST_CLEANUP_RECORDS=true|false`
- `TEST_STRICT_CLEANUP=true|false`

## Coverage Script

Script: `tests/coverage-test.sh`

This is an extended developer script for broader integration/coverage scenarios. It is environment-specific and heavier than the e2e suites above.
