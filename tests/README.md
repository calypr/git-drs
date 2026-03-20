# Git DRS

---
# NOTICE

git-drs is not yet fully compliant with DRS. It currently works against Gen3's indexd system. Full GA4GH DRS support is expected once v1.6 of the specification has been published.

---

[![Tests](https://github.com/calypr/git-drs/actions/workflows/test.yaml/badge.svg)](https://github.com/calypr/git-drs/actions/workflows/test.yaml)

## Manual External Gen3 E2E

Use [`tests/e2e-gen3-remote-full.sh`](/Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs/tests/e2e-gen3-remote-full.sh) for a comprehensive developer-run test against a live Gen3-mode drs-server (not for CI).

## Manual Local E2E

Use [`tests/e2e-local-full.sh`](/Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs/tests/e2e-local-full.sh) against a locally running `drs-server`.

Basic:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

DRS_URL='http://127.0.0.1:8080' \
BUCKET='cbds' \
ORGANIZATION='cbdsTest' \
PROJECT='git_drs_e2e_test' \
UPLOAD_MULTIPART_THRESHOLD_MB=5 \
DOWNLOAD_MULTIPART_THRESHOLD_MB=5 \
LARGE_FILE_MB=12 \
bash tests/e2e-local-full.sh
```

Optional bucket credential bootstrap during test setup:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

DRS_URL='http://127.0.0.1:8080' \
CREATE_BUCKET_BEFORE_TEST=true \
TEST_BUCKET_NAME='cbds-e2e-local' \
TEST_BUCKET_REGION='us-east-1' \
TEST_BUCKET_ACCESS_KEY='<access-key>' \
TEST_BUCKET_SECRET_KEY='<secret-key>' \
TEST_BUCKET_ENDPOINT='' \
DELETE_TEST_BUCKET_AFTER=true \
ORGANIZATION='cbdsTest' \
PROJECT='git_drs_e2e_test' \
UPLOAD_MULTIPART_THRESHOLD_MB=5 \
DOWNLOAD_MULTIPART_THRESHOLD_MB=5 \
LARGE_FILE_MB=12 \
bash tests/e2e-local-full.sh
```

Local E2E env vars:

- `ENV_FILE` (optional path to env file; defaults to `<git-drs-root>/.env`)
- `FULL_SERVER_SWEEP` (`true|false`, default `true`) to run comprehensive server endpoint checks after push/pull.
- `EXTRA_SMALL_FILES` (default `3`) additional small files to increase dataset realism.
- `EXTRA_SMALL_FILE_KB` (default `256`) size per extra small file.
- `EXTRA_LARGE_FILES` (default `1`) additional multipart-sized files.
- `EXTRA_LARGE_FILE_MB` (default `8`) size per extra large file.
- `CREATE_BUCKET_BEFORE_TEST` (`true|false`, default `false`)
- `TEST_BUCKET_NAME` (default `BUCKET`)
- `TEST_BUCKET_REGION` (default `us-east-1`)
- `TEST_BUCKET_ACCESS_KEY` (required when `CREATE_BUCKET_BEFORE_TEST=true`)
- `TEST_BUCKET_SECRET_KEY` (required when `CREATE_BUCKET_BEFORE_TEST=true`)
- `TEST_BUCKET_ENDPOINT` (optional)
- `DELETE_TEST_BUCKET_AFTER` (`true|false`, default `false`)
- `ADMIN_AUTH_HEADER` (optional raw header, e.g. `Authorization: Bearer <token>`)
- `CLEANUP_RECORDS` (`true|false`, default `true`) delete test-created DRS/index records on exit.
- `STRICT_CLEANUP` (`true|false`, default `true`) require cleanup calls to succeed (`false` allows best-effort cleanup status codes).

The local script auto-loads env vars from `.env` in the `git-drs` repo root (or `ENV_FILE` if set).

Example:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

GEN3_TOKEN='<bearer-token>' \
ORGANIZATION='cbdsTest' \
PROJECT_ID='git_drs_e2e_test' \
BUCKET='cbds' \
DRS_URL='https://caliper-training.ohsu.edu' \
UPLOAD_MULTIPART_THRESHOLD_MB=5 \
DOWNLOAD_MULTIPART_THRESHOLD_MB=5 \
LARGE_FILE_MB=12 \
bash tests/e2e-gen3-remote-full.sh
```

Optional:

- `KEEP_WORKDIR=true` to inspect artifacts after run.
- `RUN_OPTIONAL_MUTATIONS=true` to include write/update endpoint checks requiring broader privileges.
- `CREATE_BUCKET_BEFORE_TEST=true` to create a bucket credential on drs-server first and run the test against it.
- `ENV_FILE=/path/to/.env` to load shared env vars; default is `<git-drs-root>/.env`.
- `FULL_SERVER_SWEEP=true|false` (default `true`) to enable/disable expanded endpoint sweeps.
- `EXTRA_SMALL_FILES`, `EXTRA_SMALL_FILE_KB`, `EXTRA_LARGE_FILES`, `EXTRA_LARGE_FILE_MB` to control dataset richness for endpoint sweeps.
- `CLEANUP_RECORDS=true|false` (default `true`) to remove test-created records from drs-server at script exit.
- `STRICT_CLEANUP=true|false` (default `true`) to enforce successful cleanup responses.

Bucket lifecycle example:

```bash
cd /Users/peterkor/Desktop/BMEG/drs-server-complex/git-drs

GEN3_TOKEN='<bearer-token>' \
ORGANIZATION='cbdsTest' \
PROJECT_ID='git_drs_e2e_test' \
BUCKET='cbds' \
DRS_URL='https://caliper-training.ohsu.edu' \
CREATE_BUCKET_BEFORE_TEST=true \
TEST_BUCKET_NAME='cbds-e2e-temporary' \
TEST_BUCKET_REGION='us-east-1' \
TEST_BUCKET_ACCESS_KEY='<access-key>' \
TEST_BUCKET_SECRET_KEY='<secret-key>' \
TEST_BUCKET_ENDPOINT='https://s3.amazonaws.com' \
TEST_BUCKET_ORGANIZATION='cbdsTest' \
TEST_BUCKET_PROJECT_ID='git_drs_e2e_test' \
DELETE_TEST_BUCKET_AFTER=true \
bash tests/e2e-gen3-remote-full.sh
```

**Git-LFS file management for DRS servers**

Git DRS combines the power of [Git LFS](https://git-lfs.com/) with [DRS (Data Repository Service)](https://ga4gh.github.io/data-repository-service-schemas/) to manage large data files alongside your code in a single Git repository. It provides seamless integration with Gen3-backed workflows while maintaining your familiar Git workflow.

## Key Features

- **Unified Workflow**: Manage both code and large data files using standard Git commands
- **DRS Integration**: Built-in support for Gen3 DRS servers
- **Multi-Remote Support**: Work with development, staging, and production servers in one repository
- **Automatic Processing**: Files are processed automatically during commits and pushes
- **Flexible Tracking**: Track individual files, patterns, or entire directories

## How It Works

Git DRS extends Git LFS by:

1. **Initialization**: Set up repository and DRS server configuration
2. **Automatic Commits**: Create DRS objects during pre-commit hooks
3. **Automatic Pushes**: Register files with DRS servers and upload to configured storage
4. **On-Demand Downloads**: Pull specific files or patterns as needed

## Quick Start

### Installation

```bash
# Install Git LFS first
brew install git-lfs  # macOS
git lfs install --skip-smudge

# Install Git DRS
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)" -- $GIT_DRS_VERSION
```

### Basic Usage

```bash
# Initialize repository (one-time Git repo setup)
git drs init

# Add DRS remote
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --url https://calypr-public.ohsu.edu \
    --organization my-program \
    --project my-project \
    --bucket my-bucket

# Track files
git lfs track "*.bam"
git add .gitattributes

# Add and commit files
git add my-file.bam
git commit -m "Add data file"
git push

# Download files
git lfs pull -I "*.bam"
```

## Documentation

For detailed setup and usage information:

- **[Getting Started](docs/getting-started.md)** - Repository setup and basic workflows
- **[Commands Reference](docs/commands.md)** - Complete command documentation
- **[Installation Guide](docs/installation.md)** - Platform-specific installation
- **[Troubleshooting](docs/troubleshooting.md)** - Common issues and solutions
- **[S3 Integration](docs/adding-s3-files.md)** - Adding files via S3 URLs
- **[Developer Guide](docs/developer-guide.md)** - Internals and development

## Supported Servers

- **Gen3 Data Commons** (e.g., CALYPR)

## Supported Environments

- **Local Development** environments
- **HPC Systems** (e.g., ARC)

## Commands Overview

| Command                | Description                           |
| ---------------------- | ------------------------------------- |
| `git drs init`         | Initialize repository                 |
| `git drs remote add`   | Add a DRS remote server               |
| `git drs remote list`  | List configured remotes               |
| `git drs remote set`   | Set default remote                    |
| `git drs add-url`      | Add files via S3 URLs                 |
| `git lfs track`        | Track file patterns with LFS          |
| `git lfs ls-files`     | List tracked files                    |
| `git lfs pull`         | Download tracked files                |
| `git drs fetch`        | Fetch metadata from DRS server        |
| `git drs push`         | Push objects to DRS server            |

Use `--help` with any command for details. See [Commands Reference](docs/commands.md) for complete documentation.

## Requirements

- Git LFS installed and configured
- Access credentials for your DRS server
- Go 1.24+ (for building from source)

## Support

- **Issues**: [GitHub Issues](https://github.com/calypr/git-drs/issues)
- **Releases**: [GitHub Releases](https://github.com/calypr/git-drs/releases)
- **Documentation**: See `docs/` folder for detailed guides

## License

This project is part of the CALYPR data commons ecosystem.
