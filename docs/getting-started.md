# Getting Started

This guide walks you through initializing Git DRS repositories and performing common workflows.

## Repository Initialization

Every Git repository using Git DRS must be initialized, whether you're creating a new repo or cloning an existing one.

### Cloning Existing Repository (Gen3)

1. **Clone the Repository**
   ```bash
   git clone <repo-clone-url>.git
   cd <name-of-repo>
   ```

2. **Configure SSH (for SSH URLs)**
   If using SSH URLs like `git@github.com:user/repo.git`, add to `~/.ssh/config`:
   ```
   Host github.com
       TCPKeepAlive yes
       ServerAliveInterval 30
   ```

3. **Get Credentials**
   - Log in to your data commons (e.g., https://calypr-public.ohsu.edu/)
   - Profile → Create API Key → Download JSON
   - Note the path for initialization
   - **Note**: Credentials expire after 30 days

4. **Verify Configuration**
   ```bash
   git drs list-config
   ```
   Ensure `current_server` is `gen3` and `servers.gen3` contains endpoint, project ID, and bucket.

5. **Initialize Session**
   ```bash
   git drs init --cred /path/to/downloaded/credentials.json
   ```

### New Repository Setup (Gen3)

1. **Create GitHub Repository**

2. **Clone and Navigate**
   ```bash
   git clone <repo-clone-url>.git
   cd <name-of-repo>
   ```

3. **Configure SSH** (if needed - same as above)

4. **Get Credentials** (same as above)

5. **Get Project Details**
   Contact your data coordinator for:
   - Website URL
   - Project ID  
   - Bucket name

6. **Initialize Git DRS**
   ```bash
   git drs init --profile <data_commons_name> \
                --url https://datacommons.com/ \
                --cred /path/to/credentials.json \
                --project <project_id> \
                --bucket <bucket_name>
   ```

7. **Verify Configuration**
   ```bash
   git drs list-config
   ```

## File Tracking

Git DRS uses Git LFS to track files. You must explicitly track file patterns before adding them.

### View Current Tracking

```bash
git lfs track
```

### Track Files

**Single File**
```bash
git lfs track path/to/specific-file.txt
git add .gitattributes
```

**File Pattern**
```bash
git lfs track "*.bam"
git add .gitattributes
```

**Directory**
```bash
git lfs track "data/**"
git add .gitattributes
```

### Untrack Files

```bash
# View tracked patterns
git lfs track

# Remove pattern
git lfs untrack "*.bam"

# Stage changes
git add .gitattributes
```

## Basic Workflows

### Adding and Pushing Files

```bash
# Verify configuration
git drs list-config
git drs init --cred /path/to/credentials.json

# Track file type (if not already tracked)
git lfs track "*.bam"
git add .gitattributes

# Add your file
git add myfile.bam

# Verify LFS is tracking it
git lfs ls-files

# Commit and push
git commit -m "Add new data file"
git push
```

> **Note**: Git DRS automatically creates DRS records during commit and uploads files during push.

### Downloading Files

**Single File**
```bash
git lfs pull -I path/to/file.bam
```

**Pattern**
```bash
git lfs pull -I "*.bam"
```

**All Files**
```bash
git lfs pull
```

**Directory**
```bash
git lfs pull -I "data/**"
```

### Checking File Status

```bash
# List all LFS-tracked files
git lfs ls-files

# Check specific pattern
git lfs ls-files -I "*.bam"

# View localization status
# (-) = not localized, (*) = localized
git lfs ls-files
```

## Working with S3 Files

You can add references to existing S3 files without copying them:

```bash
# Track the file pattern first
git lfs track "myfile.txt"
git add .gitattributes

# Add S3 reference
git drs add-url s3://bucket/path/to/file \
  --sha256 <file-hash> \
  --aws-access-key <key> \
  --aws-secret-key <secret>

# Commit and push
git commit -m "Add S3 file reference"
git push
```

See [S3 Integration Guide](adding-s3-files.md) for detailed examples.

## Configuration Management

### View Configuration
```bash
git drs list-config
```

### Update Configuration
```bash
# Refresh credentials
git drs init --cred /path/to/new-credentials.json

# Change server
git drs init --server anvil --terraProject <project-id>
```

### Configuration Locations
- Global config: `~/.drs/config`
- Repository config: `.drs/config`
- Logs: `.drs/` directory

## Command Summary

| Action | Commands |
|--------|----------|
| **Initialize** | `git drs init --cred <file>` |
| **Track files** | `git lfs track "pattern"` |
| **Add files** | `git add file.ext` |
| **Commit** | `git commit -m "message"` |
| **Push** | `git push` |
| **Download** | `git lfs pull -I "pattern"` |
| **Check status** | `git lfs ls-files` |
| **View config** | `git drs list-config` |

## Session Workflow

For each work session:

1. **Initialize credentials** (if expired)
   ```bash
   git drs init --cred /path/to/credentials.json
   ```

2. **Work with files** (track, add, commit, push)

3. **Download files as needed**
   ```bash
   git lfs pull -I "required-files*"
   ```

## Next Steps

- [Commands Reference](commands.md) - Complete command documentation
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
- [Developer Guide](developer-guide.md) - Advanced usage and internals
