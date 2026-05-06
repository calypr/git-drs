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
   - Organization name
   - Project ID
   - Bucket name
   - Confirmation that bucket mapping exists for your organization/project

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

   **Important:** `git drs remote add` alone is not enough. Push/pull requires an existing bucket mapping for your `organization/project` (usually provisioned once by a steward/admin).

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

Git DRS uses Git-compatible pointer files. You must explicitly track file patterns before adding managed files.

### View Current Tracking

```bash
git drs track
```

### Track Files

**Single File**

```bash
git drs track path/to/specific-file.txt
git add .gitattributes
```

**File Pattern**

```bash
git drs track "*.bam"
git add .gitattributes
```

**Directory**

```bash
git drs track "data/**"
git add .gitattributes
```

### Untrack Files

```bash
# View tracked patterns
git drs track

# Remove pattern
git drs untrack "*.bam"

# Stage changes
git add .gitattributes
```

## Basic Workflows

### Adding and Pushing Files

```bash
# Track file type (if not already tracked)
git drs track "*.bam"
git add .gitattributes

# Add your file
git add myfile.bam

# Verify it is tracked
git drs ls-files

# Commit and push
git commit -m "Add new data file"
git push
```

> **Note**: Git DRS automatically creates DRS records during commit and uploads files to the default remote during push.

### Downloading Files

**All Files**

```bash
git drs pull
```

### Checking File Status

```bash
# List all tracked files
git drs ls-files
```

## Working with Cloud Object URLs

You can add references to existing bucket objects without copying them:

```bash
# Track the file pattern first
git drs track "myfile.txt"
git add .gitattributes

# Add object reference (known sha256 path)
git drs add-url s3://bucket/path/to/file \
  --sha256 <file-hash>

# Or use unknown-sha (experimental sentinel mode)
git drs add-url s3://bucket/path/to/file

# Commit and push
git commit -m "Add S3 file reference"
git push
```

See [Cloud URL Integration Guide](adding-s3-files.md) for detailed examples.

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
| **Track files**    | `git drs track "pattern"`                   |
| **Check tracked**  | `git drs ls-files`                          |
| **Add files**      | `git add file.ext`                          |
| **Commit**         | `git commit -m "message"`                   |
| **Push**           | `git push`                                  |
| **Download**       | `git drs pull`                              |

## Session Workflow

> **Note**: You do NOT need to run `git drs init` again. Initialization is a one-time setup per Git repository clone.

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

## Local DRS Server Setup

Use this flow when developing against a local `drs-server` instead of hosted Gen3.

1. **Initialize repo**

   ```bash
   git drs init
   ```

2. **Add local remote**

   ```bash
   git drs remote add local origin http://localhost:8080 \
       --organization calypr \
       --project end_to_end_test \
       --bucket cbds \
       --username drs-user \
       --password drs-pass
   ```

   If your local server has no basic auth, omit `--username/--password`.

3. **Track and push**

   ```bash
   git drs track "*.bin"
   git add .gitattributes data/example.bin
   git commit -m "Add local DRS test file"
   git drs push
   ```

4. **Verify pull**

   ```bash
   git drs pull
   ```

For complete local/remote mode behavior and e2e runbooks, see [E2E Modes + Local Setup](e2e-modes-and-local-setup.md).

3. **Download files as needed**

   ```bash
   git drs pull
   ```

## Next Steps

- [Commands Reference](commands.md) - Complete command documentation
- [Troubleshooting](troubleshooting.md) - Common issues and solutions
- [Developer Guide](developer-guide.md) - Advanced usage and internals
