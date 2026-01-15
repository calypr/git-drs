# Getting Started

This guide walks you through setting up Git DRS and performing common workflows.

> **Navigation:** [Installation](installation.md) → **Getting Started** → [Commands Reference](commands.md) → [Troubleshooting](troubleshooting.md)

## Repository Initialization

Every Git repository using Git DRS requires configuration, whether you're creating a new repo or cloning an existing one.

### Cloning Existing Repository (Gen3)

1. **Clone the Repository**

   ```bash
   git clone <repo-clone-url>.git
   cd <name-of-repo>
   ```

2. **Configure SSH** (if using SSH URLs)

   If using SSH URLs like `git@github.com:user/repo.git`, add to `~/.ssh/config`:

   ```
   Host github.com
       TCPKeepAlive yes
       ServerAliveInterval 30
   ```

3. **Get Credentials**

   - Log in to your data commons (e.g., https://calypr-public.ohsu.edu/)
   - Profile → Create API Key → Download JSON
   - **Note**: Credentials expire after 30 days

4. **Initialize Repository**

   ```bash
   git drs init
   ```

5. **Verify Configuration**

   ```bash
   git drs remote list
   ```

   Output:
   ```
   * production  gen3    https://calypr-public.ohsu.edu/
   ```

   The `*` indicates this is the default remote.

### New Repository Setup (Gen3)

1. **Create and Clone Repository**

   ```bash
   git clone <repo-clone-url>.git
   cd <name-of-repo>
   ```

2. **Configure SSH** (if needed - same as above)

3. **Get Credentials** (same as above)

4. **Get Project Details**

   Contact your data coordinator for:
   - DRS server URL
   - Project ID
   - Bucket name

5. **Initialize Git DRS**

   ```bash
   git drs init
   ```

6. **Add Remote Configuration**

   ```bash
   git drs remote add gen3 production \
       --cred /path/to/credentials.json \
       --url https://calypr-public.ohsu.edu \
       --project my-project \
       --bucket my-bucket
   ```

   **Note:** Since this is your first remote, it automatically becomes the default. No need to run `git drs remote set`.

7. **Verify Configuration**

   ```bash
   git drs remote list
   ```

   Output:
   ```
   * production  gen3    https://calypr-public.ohsu.edu
   ```

**Managing Additional Remotes**

You can add more remotes later for multi-environment workflows (development, staging, production):

```bash
# Add staging remote
git drs remote add gen3 staging \
    --cred /path/to/staging-credentials.json \
    --url https://staging.calypr.ohsu.edu \
    --project staging-project \
    --bucket staging-bucket

# View all remotes
git drs remote list

# Switch default remote
git drs remote set staging

# Or use specific remote for one command
git drs push production
git drs fetch staging
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

> **Note**: Git DRS automatically creates DRS records during commit and uploads files to the default remote during push.

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
git drs remote list
```

### Update Configuration

```bash
# Refresh credentials - re-add remote with new credentials
git drs remote add gen3 production \
    --cred /path/to/new-credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket

# Switch default remote
git drs remote set staging
```

### View Logs

- Logs location: `.git/drs/` directory

## Command Summary

| Action             | Commands                                    |
| ------------------ | ------------------------------------------- |
| **Initialize**     | `git drs init`                              |
| **Add remote**     | `git drs remote add gen3 <name> --cred...` |
| **View remotes**   | `git drs remote list`                       |
| **Set default**    | `git drs remote set <name>`                 |
| **Track files**    | `git lfs track "pattern"`                   |
| **Check tracked**  | `git lfs ls-files`                          |
| **Add files**      | `git add file.ext`                          |
| **Commit**         | `git commit -m "message"`                   |
| **Push**           | `git push`                                  |
| **Download**       | `git lfs pull -I "pattern"`                 |

## Session Workflow

For each work session:

1. **Refresh credentials** (if expired - credentials expire after 30 days)

   ```bash
   git drs remote add gen3 production \
       --cred /path/to/new-credentials.json \
       --url https://calypr-public.ohsu.edu \
       --project my-project \
       --bucket my-bucket
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
