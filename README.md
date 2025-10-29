# Git DRS

[![Tests](https://github.com/calypr/git-drs/actions/workflows/test.yaml/badge.svg)](https://github.com/calypr/git-drs/actions/workflows/test.yaml)

**Git-LFS file management for DRS servers**

Git DRS combines the power of [Git LFS](https://git-lfs.com/) with [DRS (Data Repository Service)](https://ga4gh.github.io/data-repository-service-schemas/) to manage large data files alongside your code in a single Git repository. It provides seamless integration with data platforms like Gen3 and AnVIL while maintaining your familiar Git workflow.

## Key Features

- **Unified Workflow**: Manage both code and large data files using standard Git commands
- **DRS Integration**: Built-in support for Gen3 and AnVIL DRS servers
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

# Install Git DRS (replace with desired version)
export GIT_DRS_VERSION=0.2.2
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)" -- $GIT_DRS_VERSION
```

### Basic Usage

```bash
# Initialize repository
git drs init --cred /path/to/credentials.json

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

For detailed setup and usage information, see the documentation:

- **[Installation Guide](docs/installation.md)** - Platform-specific installation instructions
- **[Getting Started](docs/getting-started.md)** - Repository initialization and basic workflows
- **[Common Commands](docs/commands.md)** - Complete command reference and examples
- **[S3 Integration](docs/adding-s3-files.md)** - Adding files via S3 URLs
- **[Troubleshooting](docs/troubleshooting.md)** - Common issues and solutions
- **[Developer Guide](docs/developer-guide.md)** - Internals and development information

## Supported Server

- **Gen3 Data Commons** (eg CALYPR)
- **AnVIL/Terra** DRS servers


## Supported Environments
- **Local Development** environments
- **HPC Systems** (eg ARC)

## Commands Overview

| Command | Description |
|---------|-------------|
| `git drs init` | Initialize repository with DRS configuration |
| `git drs list-config` | View current configuration |
| `git drs add-url` | Add files via S3 URLs |
| `git lfs track` | Track file patterns with LFS |
| `git lfs pull` | Download tracked files |
| `git lfs ls-files` | List tracked files |

Use `--help` with any command for detailed usage information.

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
