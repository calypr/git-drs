# Commands Reference

Complete reference for Git DRS and related Git LFS commands.

Git DRS owns Git/DRS orchestration and local metadata. Direct provider access, signed URL behavior, and cloud inspection are client-side responsibilities reached through `syfon/client`.

> **Navigation:** [Getting Started](getting-started.md) → **Commands Reference** → [Troubleshooting](troubleshooting.md)

## Git DRS Commands

### `git drs install`

Install global Git filter configuration for git-drs. This is equivalent in purpose to running `git-lfs install` for the git-drs filter.

**Usage:**

```bash
git drs install
```

**What it does:**

- Sets global Git config for `filter.drs.clean`
- Sets global Git config for `filter.drs.smudge`
- Sets global Git config for `filter.drs.process`
- Sets global Git config for `filter.drs.required`

**Resulting `~/.gitconfig` entries:**

```ini
[filter "drs"]
    clean = git-drs clean -- %f
    smudge = git-drs smudge -- %f
    process = git-drs filter
    required = true
```

**When to run:**

- **Once per machine/user** after installing `git-drs`
- Re-run any time you want to reset these global filter values

### `git drs init`

Initialize Git DRS in a repository. Sets up Git DRS hooks and creates a `.git/drs/` directory that Git ignores automatically.

**Usage:**

```bash
git drs init [flags]
```

**Options:**

- `--transfers <n>`: Number of concurrent transfers (default: 1)
- `--upsert`: Enable upsert for DRS objects
- `--multipart-threshold <mb>`: Multipart threshold in MB (default: 5120)
- `--enable-data-client-logs`: Enable data-client internal logs

**Example:**

```bash
git drs init
```

**What it does:**

- Creates `.git/drs/` directory structure
- Configures Git/LFS settings for git-drs managed push/pull
- Installs Git hooks for DRS workflows

**When to run:**

- **Once** after cloning a Git repository
- **Once** after creating a new Git repository
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
    --project <project-id> \
    [--organization <program>] \
    [--bucket <bucket-name>] \
    [--cred <credentials-file> | --token <token>] \
    [--url <server-url>]
```

**Options:**

- `--project <id>`: Project ID (required)
- `--cred <file>`: Path to credentials JSON file
- `--token <token>`: Token for temporary access (alternative to `--cred`)
- `--url <url>`: Optional Gen3 endpoint override
- `--organization <name>`: Program/organization scope used for bucket mapping
- `--bucket <name>`: Bucket name fallback when no org/project mapping is configured

**Examples:**

```bash
# Add production remote
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --organization my-program \
    --project my-project

# Add staging remote
git drs remote add gen3 staging \
    --cred /path/to/staging-credentials.json \
    --organization staging-program \
    --project staging-project
```

**Note:** The first remote you add automatically becomes the default remote.
**Authentication note:** Supply either `--cred` or `--token` when initially configuring a remote (or when no existing profile is available for the remote name).
**Important:** A bucket mapping for the target `organization/project` must already exist, typically created once by a steward/admin with `git drs bucket add`, then `git drs bucket add-organization` or `git drs bucket add-project --path <scheme>://<bucket>/<prefix>`. Without that mapping, push/pull operations will fail.

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
- Transfers all DRS records for a given project from the server to the local `.git/drs/lfs/objects/` directory

### `git drs add-url <object-url-or-key> [path]`

Prepare a pointer plus local DRS metadata for an object that already exists in provider storage.

**Usage:**

```bash
# Preferred: object key resolved against configured bucket scope
git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3

# Compatibility: explicit provider URL
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin
```

**Options:**

- `--scheme <scheme>`: Required for object-key mode because local bucket mappings persist bucket/prefix, not provider scheme
- `--sha256 <hex>`: Expected SHA256 checksum when known

**What it does:**

- Resolves the effective org/project bucket scope for the current remote
- Inspects the provider object through client-owned cloud code
- Writes a Git LFS pointer into the worktree
- Stores local DRS metadata for later registration during `git drs push`

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

- Checks local `.git/drs/lfs/objects/` for DRS metadata
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

### `git drs query`

Query a DRS object by its DRS ID or SHA256 checksum.

**Usage:**

```bash
# Query by DRS ID (default behavior)
git drs query <drs-id>

# Query by SHA256 checksum
git drs query --checksum <sha256>
```

**Options:**

- `--checksum`, `-c`: Treat the argument as a SHA256 checksum instead of a DRS ID.
- `--pretty`, `-p`: Output indented JSON for easier reading.
- `--remote`, `-r`: Target a specific remote (default: default_remote).

**Examples:**

```bash
# Query by checksum and pretty-print the result
git drs query --checksum 9f2c2db77f0a3e2b47e4b44b8ce8d4c8c3c4c0b5f4c5a2d2f9b1d0bfb0a1c2d3 --pretty

# Query by DRS ID against a specific remote
git drs query did:example:12345 --remote staging
```


### `git drs version`

Display Git DRS version information.

```bash
git drs version
```

### `git drs track [pattern ...]`

Manage Git LFS tracking patterns from Git DRS.

**View tracked patterns:**

```bash
git drs track
```

**Track one or more patterns:**

```bash
git drs track "*.bam"
git drs track "*.bam" "data/**"
```

**Options:**

- `--verbose`: Show detailed Git LFS output
- `--dry-run`: Show what would change without writing `.gitattributes`

### `git drs untrack <pattern> [pattern ...]`

Remove one or more Git LFS tracking patterns.

```bash
git drs untrack "*.bam"
git drs untrack "*.bam" "data/**"
```

**Options:**

- `--verbose`: Show detailed Git LFS output
- `--dry-run`: Show what would change without writing `.gitattributes`

### Internal Commands

These commands are called automatically by Git hooks:

- `git drs precommit`: Process staged files during commit
- `git drs pre-push-prepare`: Stage DRS metadata before push
- `git lfs pre-push`: Optional Git LFS compatibility push flow (invoked by the pre-push hook when enabled)

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
git drs remote add gen3 production --cred /path/to/credentials.json --url ... --organization ... --project ...
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
    --organization my-program \
    --project my-project

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
    --organization staging-program \
    --project staging-project

git drs remote add gen3 production \
    --url https://calypr-public.ohsu.edu \
    --cred /path/to/prod-credentials.json \
    --organization prod-program \
    --project prod-project

# 2. Fetch metadata from staging
git drs fetch staging

# 3. Push metadata to production (no re-upload)
git drs push production
```

## Environment Variables

Git DRS respects these environment variables:

- `AWS_ACCESS_KEY_ID`: AWS access key (for S3 operations)
- `AWS_SECRET_ACCESS_KEY`: AWS secret key (for S3 operations)

## Help and Documentation

Use `--help` with any command for detailed usage:

```bash
git-drs --help
git-drs init --help
git-drs add-url --help
git lfs --help
git lfs track --help
```
