# End-to-End Integration Test - git-drs

Location: `tests/integration/calypr/end_to_end_test.go`

## Overview

This integration test exercises a full git + Git LFS + git-drs workflow against a GitHub Enterprise Server (GHE) instance. It creates a temporary repo, configures LFS, adds a test file, attempts pushes/pulls using `git-drs`, and verifies local object storage paths and cleanup.

## What it validates

- Repository creation and deletion on the GHE org.
- `git-drs` initialization and remote configuration.
- Git LFS tracking and pointer/file handling.
- Detection and mapping of LFS objects via `drsmap.GetAllLfsFiles`.
- Basic push/pull behavior against a dummy DRS endpoint.
- Cleanup of created local repos and cloned copies.

## Prerequisites

- macOS (developer environment)
- Go toolchain (project uses Go modules)
- Binaries on `PATH`:
  - `git`
  - `git-lfs`
  - `git-drs`
  - `calypr_admin`
- Network access to the target GHE instance.
- A GitHub Personal Access Token (PAT) with permissions to create and delete repos in the target org.

## Required environment variables

- `GH_PAT` — GitHub Personal Access Token used to create/delete repos.
- `GIT_DRS_REMOTE` — name of the profile/remote used by the project (expected by config loader).

Example:
```bash
export GH_PAT="ghp_..."
export GIT_DRS_REMOTE="my-profile"
```

## Running the test

From the project root, run:

```bash
go test ./tests/integration/calypr -v -run TestEndToEndGitDRSWorkflow
```

Notes:
- The test is destructive on the GHE org (creates and removes a repository). Ensure the token and org are appropriate.
- The test runs external commands and will fail if required binaries are missing or not on `PATH`.
- The test prints temporary directories and command outputs to help debugging.

## Expected behavior

- The test should create a repo named `test-<random>` under the configured org, add a single LFS-tracked file, and exercise `git-drs` commands.
- `drsmap.GetAllLfsFiles` should return a single LFS file entry for the created test file.
- The test will attempt `git push`/`git lfs pull` which may fail against a dummy DRS server; failures are expected and asserted accordingly in the test.

## Troubleshooting

- Missing binaries: ensure required tools are installed and discoverable via `which <tool>`.
- Permission errors from GHE: verify `GH_PAT` scopes (repo creation/deletion) and that the token is valid.
- If the test fails while cleaning up, manually check and delete the temporary repo in the org.
- For verbose debugging, run the test with `-v` (already recommended).

## Safety & CI

- Consider running this test in an isolated org or a test GHE instance.
- Run in CI only if the runner has network access and safe permissions.
