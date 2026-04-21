# Developer Guide

This guide covers Git DRS internals, architecture, and development information.

## Architecture Overview

Git DRS integrates with Git through several mechanisms:

### Git Hooks Integration

**Pre-commit Hook**: `git drs precommit`
- Triggered automatically before each commit
- Processes all staged LFS files
- Creates DRS records for new files
- Only processes files that don't already exist on the DRS server
- Prepares metadata for later upload during push

**Pre-push Hook**: `git drs pre-push-prepare` (internal)
- Triggered automatically before each push
- Stages pending metadata for new/changed LFS files
- Hook then runs standard `git lfs pre-push`

**Managed Push/Pull + LFS Batch Compatibility**
- `git drs push` performs register/upload workflow directly via git-drs clients
- `git drs pull` performs download workflow directly via git-drs clients
- Standard Git LFS compatibility is provided through `/info/lfs` batch endpoints

### File Processing Flow

```
1. Developer: git add file.bam
2. Developer: git commit -m "Add data"
3. Git Hook: git drs precommit
   - Creates DRS object metadata
   - Stores in .git/drs/ directory
4. Developer: git push
5. Git Hook: git drs pre-push-prepare
   - Stages pending metadata for LFS verify
6. Git Hook: git lfs pre-push
   - Executes standard LFS push flow
7. Git DRS:
   - `git drs push` can run register/upload directly
   - `git drs pull` can run download directly
```

## Current Data Path

Git DRS no longer uses a Git LFS custom transfer agent.

- Upload path (primary): `git drs push` discovers local LFS pointers, bulk-registers missing objects, checks validity, and uploads missing bits.
- Download path (primary): `git drs pull` resolves object records and downloads into local LFS object storage.
- Compatibility path: stock `git-lfs` can use server `/info/lfs` endpoints (`objects/batch`, verify, metadata staging) for interoperability.

## Repository Structure

### Core Components

```
cmd/                    # CLI command implementations
├── initialize/         # Repository initialization
├── push/               # Register/upload workflow
├── pull/               # Download workflow
├── prepush/            # Pre-push metadata staging hook
├── precommit/         # Pre-commit hook
├── addurl/            # Cloud object URL reference handling
└── ...

client/                # DRS client implementations
├── interface.go       # Client interface definitions
├── DRS.go         # Gen3/DRS client
└── drs-map.go        # File mapping utilities

config/                # Configuration management
└── config.go         # Config file handling

drs/                   # DRS object utilities
├── object.go         # DRS object structures
└── util.go           # Utility functions

lfs/                   # Git LFS integration
└── lfs.go            # LFS pointer/discovery helpers

utils/                 # Shared utilities
├── common.go         # Common functions
├── lfs-track.go      # LFS tracking utilities
└── util.go           # General utilities
```

### Configuration System

**Repository Configuration**: `.git/drs/config.yaml`
```yaml
current_server: gen3
servers:
  gen3:
    endpoint: "https://data.example.org/"
    profile: "myprofile"
    project: "project-123"
    bucket: "data-bucket"
```

### DRS Object Management

Objects are stored in `.git/drs/lfs/objects/` during pre-commit and referenced during push/pull workflows.

## Development Setup

### Prerequisites

- Go 1.24+
- Git LFS installed
- Access to a DRS server for testing

### Building from Source

```bash
# Clone repository
git clone https://github.com/calypr/git-drs.git
cd git-drs

# Install dependencies
go mod download

# Build
go build

# Install locally
export PATH=$PATH:$(pwd)
```

### Development Workflow

1. **Make changes** to source code
2. **Build and test**:
   ```bash
   go build
   go test ./...
   ```
3. **Test with real repository**:
   ```bash
   cd /path/to/test-repo
   /path/to/git-drs/git-drs --help
   ```

## Debugging and Logging

### Log Locations

- **Commit logs**: `.git/drs/git-drs.log`
- **Push/Pull logs**: `.git/drs/git-drs.log`


## Testing

### Unit Tests

```bash
# Test specific functionality
go test ./utils -run TestLFSTrack
```

### Integration Tests

**WIP**
