# Commands Reference

Complete reference for Git DRS and related Git LFS commands.

> **Navigation:** [Getting Started](getting-started.md) → **Commands Reference** → [Troubleshooting](troubleshooting.md)

## Git DRS Commands

### `git drs init`

Initialize Git DRS in a repository. Sets up Git LFS custom transfer hooks and configures `.gitignore` patterns.

**Usage:**

```bash
git drs init [flags]
```

**Options:**

- `--transfers <n>`: Number of concurrent transfers (default: 4)

**Example:**

```bash
git drs init
```

**What it does:**

- Creates `.drs/` directory structure
- Configures Git LFS custom transfer agent
- Updates `.gitignore` to exclude DRS cache files
- Stages `.gitignore` changes automatically

**When to run:**

- **Once** after cloning a Git repository to your local machine
- **Once** after creating a new Git repository locally
- **Never** needed for subsequent work sessions

**You do NOT need to run `git drs init` again:**

- When starting a new work session
- After refreshing credentials
- After pulling new changes

**Note:** Run this before adding remotes.

### `git drs remote`

Manage DRS remote server configurations. Git DRS supports multiple remotes for working with development, staging, and production servers.

#### `git drs remote add gen3 <name>`

Add a Gen3 DRS server configuration.

**Usage:**

```bash
git drs remote add gen3 <remote-name> \
    --url <server-url> \
    --cred <credentials-file> \
    --project <project-id> \
    --bucket <bucket-name>
```

**Options:**

- `--url <url>`: Gen3 server endpoint (required)
- `--cred <file>`: Path to credentials JSON file (required)
- `--token <token>`: Token for temporary access (alternative to --cred)
- `--project <id>`: Project ID in format `<program>-<project>` (required)
- `--bucket <name>`: S3 bucket name (required)

**Examples:**

```bash
# Add production remote
git drs remote add gen3 production \
    --url https://calypr-public.ohsu.edu \
    --cred /path/to/credentials.json \
    --project my-project \
    --bucket my-bucket

# Add staging remote
git drs remote add gen3 staging \
    --url https://staging.calypr.ohsu.edu \
    --cred /path/to/staging-credentials.json \
    --project staging-project \
    --bucket staging-bucket
```

**Note:** The first remote you add automatically becomes the default remote.

#### `git drs remote add anvil <name>`

Add an AnVIL/Terra DRS server configuration.

> **Note:** AnVIL support is under active development. For production use, we recommend Gen3 workflows or version 0.2.2 for AnVIL functionality.

**Usage:**

```bash
git drs remote add anvil <remote-name> --terraProject <project-id>
```

**Options:**

- `--terraProject <id>`: Terra/Google Cloud project ID (required)

**Example:**

```bash
git drs remote add anvil development --terraProject my-terra-project
```

#### `git drs remote list`

List all configured DRS remotes.

**Usage:**

```bash
git drs remote list
```

**Example Output:**

```
* production  gen3    https://calypr-public.ohsu.edu
  staging     gen3    https://staging.calypr.ohsu.edu
  development gen3    https://dev.calypr.ohsu.edu
```

The `*` indicates the default remote used by all commands unless specified otherwise.

#### `git drs remote set <name>`

Set the default DRS remote for all operations.

**Usage:**

```bash
git drs remote set <remote-name>
```

**Examples:**

```bash
# Switch to staging for testing
git drs remote set staging

# Switch back to production
git drs remote set production

# Verify change
git drs remote list
```

### `git drs fetch [remote-name]`

Fetch DRS object metadata from remote server. Downloads metadata only, not actual files.

**Usage:**

```bash
# Fetch from default remote
git drs fetch

# Fetch from specific remote
git drs fetch staging
git drs fetch production
```

**Note:** `fetch` and `push` are commonly used together for cross-remote workflows. See `git drs push` below.

**What it does:**

- Identifies remote and project from configuration
- Transfers all DRS records for a given project from the server to the local `.drs/lfs/objects/` directory

### `git drs push [remote-name]`

Push local DRS objects to server. Uploads new files and registers metadata.

**Usage:**

```bash
# Push to default remote
git drs push

# Push to specific remote
git drs push staging
git drs push production
```

**What it does:**

- Checks local `.drs/lfs/objects/` for DRS metadata
- For each object, uploads file to bucket if file exists locally
- If file doesn't exist locally (metadata only), registers metadata without upload
- This enables cross-remote promotion workflows

**Cross-Remote Promotion:**

Transfer DRS records from one remote to another (eg staging to production) without re-uploading files:

```bash
# Fetch metadata from staging
git drs fetch staging

# Push metadata to production (no file upload since files don't exist locally)
git drs push production
```

This is useful when files are already in the production bucket with matching SHA256 hashes. It can also be used to reupload files given that the files are pulled to the repo first.

**Note:** `fetch` and `push` are commonly used together. `fetch` pulls metadata from one remote, `push` registers it to another.

### `git drs add-url`

Add a file reference via S3 URL without copying the data.

**Usage:**

```bash
# Use default remote
git drs add-url s3://bucket/path/file --sha256 <hash>

# Use specific remote
git drs add-url s3://bucket/path/file --sha256 <hash> --remote staging
```

**With AWS Credentials:**

```bash
git drs add-url s3://bucket/path/file \
  --sha256 <hash> \
  --aws-access-key <key> \
  --aws-secret-key <secret>
```

**Options:**

- `--sha256 <hash>`: Required SHA256 hash of the file
- `--remote <name>`: Target remote (default: default_remote)
- `--aws-access-key <key>`: AWS access key
- `--aws-secret-key <secret>`: AWS secret key
- `--endpoint <url>`: Custom S3 endpoint
- `--region <region>`: AWS region

### `git drs create-cache`

Create a cache from a manifest file (Terra/AnVIL).

```bash
git drs create-cache manifest.tsv
```

### `git drs version`

Display Git DRS version information.

```bash
git drs version
```

### Internal Commands

These commands are called automatically by Git hooks:

- `git drs precommit`: Process staged files during commit
- `git drs transfer`: Handle file transfers during push/pull
- `git drs transferref`: Handle reference transfers (AnVIL/Terra)

## Git LFS Commands

### `git lfs track`

Manage file tracking patterns.

**View Tracked Patterns:**

```bash
git lfs track
```

**Track New Pattern:**

```bash
git lfs track "*.bam"
git lfs track "data/**"
git lfs track "specific-file.txt"
```

**Untrack Pattern:**

```bash
git lfs untrack "*.bam"
```

### `git lfs ls-files`

List LFS-tracked files in the repository.

**All Files:**

```bash
git lfs ls-files
```

**Specific Pattern:**

```bash
git lfs ls-files -I "*.bam"
git lfs ls-files -I "data/**"
```

**Output Format:**

- `*` prefix: File is localized (downloaded)
- `-` prefix: File is not localized
- No prefix: File status unknown

### `git lfs pull`

Download LFS-tracked files.

**All Files:**

```bash
git lfs pull
```

**Specific Files:**

```bash
git lfs pull -I "*.bam"
git lfs pull -I "data/important.txt"
git lfs pull -I "results/**"
```

**Multiple Patterns:**

```bash
git lfs pull -I "*.bam" -I "*.vcf"
```

### `git lfs install`

Configure Git LFS for the system or repository.

**System-wide:**

```bash
git lfs install --skip-smudge
```

**Repository-only:**

```bash
git lfs install --local --skip-smudge
```

The `--skip-smudge` option prevents automatic downloading of all LFS files during clone/checkout.

## Standard Git Commands

Git DRS integrates with standard Git commands:

### `git add`

Stage files for commit. LFS-tracked files are automatically processed.

```bash
git add myfile.bam
git add data/
git add .
```

### `git commit`

Commit changes. Git DRS pre-commit hook runs automatically.

```bash
git commit -m "Add new data files"
```

### `git push`

Push commits to remote. Git DRS automatically uploads new files to DRS server.

```bash
git push
git push origin main
```

### `git clone`

Clone repository. Use with Git DRS initialization:

```bash
git clone <repo-url>
cd <repo-name>
git drs init
git drs remote add gen3 production --cred /path/to/credentials.json --url ... --project ... --bucket ...
```

## Workflow Examples

### Complete File Addition Workflow

```bash
# 1. Ensure file type is tracked
git lfs track "*.bam"
git add .gitattributes

# 2. Add your file
git add mydata.bam

# 3. Verify tracking
git lfs ls-files -I "mydata.bam"

# 4. Commit (creates DRS record)
git commit -m "Add analysis results"

# 5. Push (uploads to default DRS server)
git push
```

### Selective File Download

```bash
# Check what's available
git lfs ls-files

# Download specific files
git lfs pull -I "results/*.txt"
git lfs pull -I "important-dataset.bam"

# Verify download
git lfs ls-files -I "results/*.txt"
```

### Repository Setup from Scratch

```bash
# 1. Create and clone repo
git clone <new-repo-url>
cd <repo-name>

# 2. Initialize Git DRS
git drs init

# 3. Add DRS remote
git drs remote add gen3 production \
    --url https://calypr-public.ohsu.edu \
    --cred /path/to/credentials.json \
    --project my-project \
    --bucket my-bucket

# 4. Set up file tracking
git lfs track "*.bam"
git lfs track "*.vcf.gz"
git lfs track "data/**"
git add .gitattributes
git commit -m "Configure LFS tracking"
git push

# 5. Add data files
git add data/sample1.bam
git commit -m "Add sample data"
git push
```

### Cross-Remote Promotion Workflow

```bash
# 1. Add multiple remotes
git drs remote add gen3 staging \
    --url https://staging.calypr.ohsu.edu \
    --cred /path/to/staging-credentials.json \
    --project staging-project \
    --bucket staging-bucket

git drs remote add gen3 production \
    --url https://calypr-public.ohsu.edu \
    --cred /path/to/prod-credentials.json \
    --project prod-project \
    --bucket prod-bucket

# 2. Fetch metadata from staging
git drs fetch staging

# 3. Push metadata to production (no re-upload)
git drs push production
```

## Environment Variables

Git DRS respects these environment variables:

- `AWS_ACCESS_KEY_ID`: AWS access key (for S3 operations)
- `AWS_SECRET_ACCESS_KEY`: AWS secret key (for S3 operations)
- `GOOGLE_PROJECT`: Google Cloud project ID (for AnVIL)
- `WORKSPACE_BUCKET`: Terra workspace bucket (for AnVIL)

## Help and Documentation

Use `--help` with any command for detailed usage:

```bash
git-drs --help
git-drs init --help
git-drs add-url --help
git lfs --help
git lfs track --help
```
