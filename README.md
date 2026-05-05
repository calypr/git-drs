# Git DRS

---
# NOTICE

git-drs is not yet fully compliant with DRS. It currently works against Gen3 DRS server. Full GA4GH DRS support is expected once v1.6 of the specification has been published.

---

[![Tests](https://github.com/calypr/git-drs/actions/workflows/test.yaml/badge.svg)](https://github.com/calypr/git-drs/actions/workflows/test.yaml)

**Git/DRS orchestration with optional Git LFS compatibility**

Git DRS manages Git-facing DRS workflows: local metadata, Git hooks, filter behavior, lookup/register/push/pull orchestration, and optional Git LFS compatibility. Provider-specific transfer, signed URL behavior, and direct cloud inspection live in client code outside this repo.

## Key Features

- **Unified Workflow**: Manage both code and large data files using standard Git commands
- **DRS Integration**: Built-in support for Gen3 DRS servers
- **Multi-Remote Support**: Work with development, staging, and production servers in one repository
- **Automatic Processing**: Files are processed automatically during commits and pushes
- **Flexible Tracking**: Track individual files, patterns, or entire directories

## How It Works

Git DRS works alongside Git LFS when you want LFS-compatible pointers and storage, while still supporting DRS-centric workflows:

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

# Install global Git filter configuration for git-drs
git drs install
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

# Required prerequisite (usually steward/admin setup):
# create bucket credentials, then map org/project to full storage roots before users run push/pull
git drs bucket add production \
    --bucket my-bucket \
    --region us-east-1 \
    --access-key "$AWS_ACCESS_KEY_ID" \
    --secret-key "$AWS_SECRET_ACCESS_KEY" \
    --s3-endpoint https://s3.amazonaws.com
git drs bucket add-organization production \
    --organization my-program \
    --path s3://my-bucket/my-program
git drs bucket add-project production \
    --organization my-program \
    --project my-project \
    --path s3://my-bucket/my-program/my-project

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
- **[E2E Modes + Local Setup](docs/e2e-modes-and-local-setup.md)** - Local vs remote mode, server config, and end-to-end runbooks
- **[Cloud/Object Integration](docs/adding-s3-files.md)** - Adding files from provider URLs or configured bucket object keys
- **[Developer Guide](docs/developer-guide.md)** - Internals and development

## Supported Servers

- **Gen3 Data Commons** (e.g., CALYPR)

## Supported Environments

- **Local Development** environments
- **HPC Systems** (e.g., ARC)

## Commands Overview

| Command                | Description                           |
| ---------------------- | ------------------------------------- |
| `git drs install`      | Install global git-drs filter config  |
| `git drs init`         | Initialize repository                 |
| `git drs remote add`   | Add a DRS remote server               |
| `git drs remote list`  | List configured remotes               |
| `git drs remote set`   | Set default remote                    |
| `git drs add-url`      | Add files via provider URLs or configured bucket object keys |
| `git lfs track`        | Track file patterns with LFS          |
| `git lfs ls-files`     | List tracked files                    |
| `git lfs pull`         | Download tracked files                |
| `git drs fetch`        | Fetch metadata from DRS server        |
| `git drs push`         | Push objects to DRS server            |

Use `--help` with any command for details. See [Commands Reference](docs/commands.md) for complete documentation.

## Requirements

- Git LFS installed and configured
- Access credentials for your DRS server
- Go 1.26.2+ (for building from source)

## Support

- **Issues**: [GitHub Issues](https://github.com/calypr/git-drs/issues)
- **Releases**: [GitHub Releases](https://github.com/calypr/git-drs/releases)
- **Documentation**: See `docs/` folder for detailed guides

## License

This project is part of the CALYPR data commons ecosystem.
