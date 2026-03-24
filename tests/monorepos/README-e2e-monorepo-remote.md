# Monorepo-Scale E2E (git-drs)

This runner extends the `git-drs/tests/monorepos` workflow for realistic-scale end-to-end validation where:
- many LFS files are generated across multiple top-level trees,
- `git drs push` is exercised repeatedly (per tree or all-at-once),
- fresh clone + `git drs pull` validates hydration behavior at scale.

## Files

- `e2e-monorepo-remote.sh`: main runner
- `generate-fixtures.go`: fixture generator
- `Makefile`: optional helper targets to build generator / produce sample fixtures

## Required env vars

- `TEST_ORGANIZATION`
- `TEST_PROJECT_ID`
- `TEST_BUCKET`
- `MONO_REMOTE_URL` (Git remote URL to push into)
- `TEST_GITHUB_TOKEN` (recommended for private GitHub repos)
  - When `MONO_REMOTE_URL` is a GitHub URL and repo does not exist, the runner will attempt to create it (private) using this token.

Auth:
- Use `TEST_GEN3_TOKEN`, or
- set `GEN3_PROFILE` / `TEST_GEN3_PROFILE` and a valid `~/.gen3/gen3_client_config.ini`.
- Optional: `ENV_FILE=/path/to/.env` to force env-file loading.
  - Default behavior when `ENV_FILE` is unset: load `git-drs/.env` only (if present).

## Common optional env vars

- `TEST_DRS_URL` (default `https://caliper-training.ohsu.edu`)
- `TEST_SERVER_MODE` (`remote` default)
- `MONO_TOP_LEVELS` (comma-separated, default `TARGET-ALL-P2,TCGA-GBM,TCGA-LUAD`)
- `MONO_SUBDIRS` (default `2`)
- `MONO_FILES_PER_SUBDIR` (default `20`)
- `MONO_PUSH_PER_DIR` (`true` default; pushes after each top-level tree)
- `MONO_RUN_CLONE_VERIFY` (`true` default)
- `MONO_KEEP_WORKDIR` (`false` default)
- `MONO_WORK_ROOT` (temp dir default)
- `MONO_MULTIPART_THRESHOLD_MB` (default `64`; keeps generated ~20MB files single-part)
- `MONO_RUN_MULTIPART_SMOKE` (default `true`; runs one forced multipart upload)
- `MONO_MULTIPART_SMOKE_MB` (default `96`; size of multipart smoke file)
- `TEST_CREATE_BUCKET_BEFORE_TEST` (`false` default)
- `TEST_DELETE_BUCKET_AFTER` (`true` default when bucket was created by test)
- `TEST_BUCKET_NAME`, `TEST_BUCKET_REGION`, `TEST_BUCKET_ACCESS_KEY`, `TEST_BUCKET_SECRET_KEY`, `TEST_BUCKET_ENDPOINT` (required when create-before-test is enabled)

## Example

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

TEST_DRS_URL="https://caliper-training.ohsu.edu" \
TEST_SERVER_MODE="remote" \
GEN3_PROFILE="local" \
TEST_ORGANIZATION="calypr" \
TEST_PROJECT_ID="end_to_end_test" \
TEST_BUCKET="cbds" \
MONO_REMOTE_URL="https://github.com/calypr/git-drs-e2e-remote.git" \
MONO_TOP_LEVELS="TARGET-ALL-P2,TCGA-GBM,TCGA-LUAD,TCGA-KIRC" \
MONO_SUBDIRS=2 \
MONO_FILES_PER_SUBDIR=30 \
MONO_MULTIPART_THRESHOLD_MB=64 \
MONO_RUN_MULTIPART_SMOKE=true \
MONO_MULTIPART_SMOKE_MB=96 \
bash tests/monorepos/e2e-monorepo-remote.sh
```

If you see `credential not found` for `/data/multipart/init`, run with bucket provisioning enabled:

```bash
TEST_CREATE_BUCKET_BEFORE_TEST=true \
TEST_DELETE_BUCKET_AFTER=true \
TEST_BUCKET_NAME="cbds" \
TEST_BUCKET_REGION="us-east-1" \
TEST_BUCKET_ACCESS_KEY="cbds-user" \
TEST_BUCKET_SECRET_KEY="<secret>" \
TEST_BUCKET_ENDPOINT="https://aced-storage.ohsu.edu/" \
bash tests/monorepos/e2e-monorepo-remote.sh
```

## Notes

- This runner expects `git-drs` to already be installed in `PATH`.
- The runner uses `git drs remote add gen3|local ...` and configures LFS endpoint to `$TEST_DRS_URL/info/lfs`.
- The fixture generator intentionally alternates small and ~20 MiB files to force mixed single-part and multipart upload behavior.
