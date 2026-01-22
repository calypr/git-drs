# Git DRS

---
# NOTICE

git-drs is not yet fully compliant with DRS. It currently works against Gen3's indexd system. Full GA4GH DRS support is expected once v1.6 of the specification has been published.

---

[![Tests](https://github.com/calypr/git-drs/actions/workflows/test.yaml/badge.svg)](https://github.com/calypr/git-drs/actions/workflows/test.yaml)

**Git-LFS file management for DRS servers**

Git DRS combines the power of [Git LFS](https://git-lfs.com/) with [DRS (Data Repository Service)](https://ga4gh.github.io/data-repository-service-schemas/) to manage large data files alongside your code in a single Git repository. It provides seamless integration with data platforms like Gen3 and AnVIL while maintaining your familiar Git workflow.

## Key Features

- **Unified Workflow**: Manage both code and large data files using standard Git commands
- **DRS Integration**: Built-in support for Gen3 DRS servers (AnVIL support under active development)
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
- **AnVIL/Terra** DRS servers (under active development)

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
