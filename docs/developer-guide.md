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

**Pre-push Hook**: `git drs prepush`
- Triggered automatically before each push
- Updates DRS records for new or changed LFS files

**Custom Transfer Protocol**
- Git LFS uses custom transfers to communicate with Git DRS
- Handles both upload (push) and download (pull) operations
- Transfers run automatically during `git push` and `git lfs pull`
- Information passed through JSON protocol between Git LFS and Git DRS

### File Processing Flow

```
1. Developer: git add file.bam
2. Developer: git commit -m "Add data"
3. Git Hook: git drs precommit
   - Creates DRS object metadata
   - Stores in .drs/ directory
4. Developer: git push
5. Git Hook: git drs prepush
   - Updates DRS object metadata
6. Git LFS: Initiates custom transfer
7. Git DRS: 
   - Registers file with DRS server (indexd record)
   - Uploads file to configured bucket
   - Updates transfer logs
```

## Custom Transfer Protocol

Git DRS implements the [Git LFS Custom Transfer Protocol](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md).

### Transfer Types

**Upload Transfer (gen3)**:
- Creates indexd record on DRS server
- Uploads file to Gen3-registered S3 bucket
- Updates DRS object with access URLs

**Download Transfer (gen3)**:
- Retrieves file metadata from DRS server
- Downloads file from configured storage
- Validates checksums

**Reference Transfer**:
- Handles S3 URL references without data movement
- Links existing S3 objects to DRS records

### Protocol Communication

Git LFS and Git DRS communicate via JSON messages:

```json
{
  "event": "init",
  "operation": "upload",
  "remote": "origin",
  "concurrent": 3,
  "concurrenttransfers": 3
}
```

Response handling and logging occurs in transfer clients to avoid interfering with Git LFS stdout expectations.

## Repository Structure

### Core Components

```
cmd/                    # CLI command implementations
├── initialize/         # Repository initialization
├── transfer/          # Custom transfer handlers
├── precommit/         # Pre-commit hook
├── addurl/            # S3 URL reference handling
└── ...

client/                # DRS client implementations
├── interface.go       # Client interface definitions
├── indexd.go         # Gen3/indexd client
├── anvil.go          # AnVIL client
└── drs-map.go        # File mapping utilities

config/                # Configuration management
└── config.go         # Config file handling

drs/                   # DRS object utilities
├── object.go         # DRS object structures
└── util.go           # Utility functions

lfs/                   # Git LFS integration
└── messages.go       # LFS protocol messages

utils/                 # Shared utilities
├── common.go         # Common functions
├── lfs-track.go      # LFS tracking utilities
└── util.go           # General utilities
```

### Configuration System

**Repository Configuration**: `.drs/config`
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

Objects are stored in `.drs/objects/` during pre-commit and referenced during transfers.

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

- **Commit logs**: `.drs/git-drs.log`
- **Transfer logs**: `.drs/git-drs.log`


## Testing

### Unit Tests

```bash
# Test specific functionality
go test ./utils -run TestLFSTrack
```

### Integration Tests

**WIP**
