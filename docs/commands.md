# Commands Reference

Complete reference for Git DRS and related Git LFS commands.

## Git DRS Commands

### `git drs init`

Initialize or configure Git DRS for a repository. Required after each clone.

**Basic Usage:**

```bash
git drs init --cred /path/to/credentials.json
```

**Full Configuration:**

```bash
git drs init --profile <name> \
             --url <server-url> \
             --cred <credentials-file> \
             --project <project-id> \
             --bucket <bucket-name>
```

**Simplified Configuration:**

Use this configuration if you have already setup an authentication profile with `git drs init --cred /path/to/credentials.json --profile <name>` and you want to use that profile again:

```bash
git drs init --profile <name> --project <project-id> --bucket <bucket-name>
```

In cases where you are cloning a repository that already exists and want to keep the existing project and bucket names you will only need to do:

```bash
git drs init --profile <name>
```

**Server-Specific:**

```bash
# AnVIL server
git drs init --server anvil --terraProject <terra-project-id>

# AnVIL basic usages
git drs init --server anvil
```

**Options:**

- `--profile <name>`: Server profile name
- `--url <url>`: DRS server endpoint URL
- `--cred <file>`: Path to credentials JSON file
- `--project <id>`: Project ID
- `--bucket <name>`: Storage bucket name
- `--server <type>`: Server type (`gen3`, `anvil`)
- `--terraProject <id>`: Terra project ID for AnVIL

### `git drs list-config`

Display current Git DRS configuration.

```bash
git drs list-config
```

Shows:

- Current active server
- Configured servers and their endpoints
- Project IDs and bucket configurations
- Credential status

### `git drs add-url`

Add a file reference via S3 URL without copying the data.

**Basic Usage:**

```bash
export AWS_ACCESS_KEY_ID=<key>
export AWS_SECRET_ACCESS_KEY=<secret>

git drs add-url s3://bucket/path/file --sha256 <hash>
```

**With Credentials Flags:**

```bash
git drs add-url s3://bucket/path/file \
  --sha256 <hash> \
  --aws-access-key <key> \
  --aws-secret-key <secret>
```

**External Bucket:**

```bash
export AWS_ACCESS_KEY_ID=<key>
export AWS_SECRET_ACCESS_KEY=<secret>

git drs add-url s3://external-bucket/file \
  --sha256 <hash> \
  --endpoint https://custom-endpoint.com \
  --region us-west-2
```

**Options:**

- `--sha256 <hash>`: Required SHA256 hash of the file
- `--aws-access-key <key>`: AWS access key (overrides `AWS_ACCESS_KEY_ID`)
- `--aws-secret-key <key>`: AWS secret key (overrides `AWS_SECRET_ACCESS_KEY`)
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
git drs init --cred /path/to/credentials.json
```

## Workflow Examples

### Complete File Addition Workflow

```bash
# 0. Initialize project
git drs init --cred path/to/cred

# 1. Ensure file type is tracked
git lfs track "*.bam"
git add .gitattributes

# 2. Add your file
git add mydata.bam

# 3. Verify tracking
git lfs ls-files -I "mydata.bam"

# 4. Commit (creates DRS record)
git commit -m "Add analysis results"

# 5. Push (uploads to DRS server)
git push
```

### Selective File Download

```bash
# 0. Initialize project
git drs init --cred path/to/cred

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
git drs init --profile mylab \
             --url https://data.mylab.org/ \
             --cred /path/to/creds.json \
             --project my-project \
             --bucket my-bucket

# 3. Set up file tracking
git lfs track "*.bam"
git lfs track "*.vcf.gz"
git lfs track "data/**"
git add .gitattributes
git commit -m "Configure LFS tracking"
git push

# 4. Add data files
git add data/sample1.bam
git commit -m "Add sample data"
git push
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
