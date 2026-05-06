# Troubleshooting

Common issues and solutions when working with Git DRS.

> **Navigation:** [Getting Started](getting-started.md) → [Commands Reference](commands.md) → **Troubleshooting**

## Frequently Asked Questions

### Do I need to run `git drs init` each time?

**No.** `git drs init` is set up once per Git repo.

**Run it once when:**

- You first clone a repository
- You create a new repository

**Don't run it again:**

- At the start of each work session
- After refreshing credentials
- After pulling updates

**What it does:**

- Sets up `.git/drs/` directory structure
- Configures Git DRS repository settings and hooks
- Updates `.gitignore`

These changes persist in your local repository. For subsequent sessions, you only need to refresh credentials if they've expired (every 30 days).

### What to do if you run `git drs init` again

Running `git drs init` a second time is usually harmless but unnecessary. It may re-create the `\`.git/drs/\`` directory, re-install hooks, or modify `\`.gitattributes\`` and `\`.gitignore\``. If you ran it accidentally, follow these steps:

1. Inspect what changed
   - `git status`
   - `git diff` (or `git diff -- <file>` for a specific file, e.g. `\`.gitignore\``)

2. If changes are fine
   - No action required; commit the intended changes or leave them uncommitted.

3. If you want to discard uncommitted changes
   - Restore specific files: `git restore --staged \`.gitignore\`` && `git restore \`.gitignore\``
   - Restore all working-tree changes: `git restore .`
   - Or (destructive) reset everything: `git reset --hard`  \- use with caution.

4. If you already committed the unintended changes
   - Undo the last commit but keep changes staged: `git reset --soft HEAD~1`
   - Or remove the commit and working changes: `git reset --hard HEAD~1`  \- use with caution.
   - See the "Undo Last Commit" section above for alternatives.

5. Hooks or credentials issues
   - If hooks were replaced, re-run `git drs init`.
   - If credentials need refresh, re-add the remote with `git drs remote add` using `--cred` or `--token`.

Summary: inspect with `git status`/`git diff`, then either accept, manually edit, or revert the changes using standard `git restore` / `git reset` commands.


## When to Use Which Tool

Understanding when to use Git or Git DRS commands:

### Git DRS Commands

**Use for**: Repository and remote configuration, file tracking, and data transfer

- `git drs init` - Initialize Git DRS hooks
- `git drs remote add` - Configure DRS server connections
- `git drs remote list` - View configured remotes
- `git drs add-url` - Add cloud object references
- `git drs track` - Define which files to track
- `git drs ls-files` - See tracked files and status
- `git drs pull` - Download specific files
- `git drs untrack` - Stop tracking file patterns

**When**:

- Setting up a new repository
- Adding/managing DRS remotes
- Refreshing expired credentials
- Adding external file references

- Managing which files are stored externally
- Downloading specific files
- Checking file localization status

### Standard Git Commands

**Use for**: Version control operations

- `git add` - Stage files for commit
- `git commit` - Create commits
- `git push` - Upload commits and trigger file uploads
- `git pull` - Get latest commits

**When**:

- Normal development workflow
- Git DRS runs automatically in the background

## Common Error Messages

## Git DRS Troubleshooting Guide (Commit/Push/Clone/Pull)

### Failed Commit (hook or pointer issues)

1. **Confirm git-drs install ran and hooks are active**
   - Run: `git drs remote list` to confirm configuration is present.
   - If hooks are missing, re-run `git drs init`.

2. **Check whether the file was tracked before the commit**
   - Run: `git drs track` and confirm the file pattern is listed.
   - If not tracked, add it (`git drs track "*.bam"`) and stage `.gitattributes`.

3. **Verify the file is staged as a pointer**
   - Run: `git drs ls-files` to confirm the file is listed.
   - If a large file was added to Git history directly, remove it from the index and re-add it after tracking.

4. **Review logs for hook errors**
   - Run: `cat .git/drs/*.log` to inspect hook failures.

### Failed Push (uploads, auth, or bandwidth issues)

1. **Check authentication and endpoint configuration**
   - Run: `git drs remote list` and confirm remote values are correct.
   - If tokens are expired, refresh credentials with `git drs remote add` using `--cred` or `--token`.

2. **Retry with verbose logging**
   - Set `GIT_TRACE=1 GIT_CURL_VERBOSE=1` and re-run the push.
   - Use this output to identify `403/401` auth issues or proxy errors.

3. **Confirm the objects exist locally**
   - Run: `git drs ls-files` and ensure your large files are listed.

### Failed Clone (objects missing or blocked)

1. **Confirm objects were fetched**
   - After clone, run: `git drs pull` to fetch large files.
   - If the repo only has pointers, you will see pointer files until you pull.

2. **Validate access and authentication**
   - Run: `git drs remote list` to confirm the configured endpoint.
   - 401/403 errors point to invalid credentials; refresh with `git drs remote add`.

3. **Review logs for download errors**
   - Run: `cat .git/drs/*.log` for the most recent transfer errors.

### Failed Pull (fetch/checkout issues)

1. **Run `git drs pull` separately**
   - This isolates DRS download errors from Git merge errors.

2. **Review filters and tracking**
   - Run: `git drs track` to ensure required patterns are present.
   - If a file type is newly tracked, re-run `git add .gitattributes` and commit.

3. **Check logs**
   - Run: `cat .git/drs/*.log` for the most recent errors.

### Authentication Errors

**Error**: `Upload error: 403 Forbidden` or `401 Unauthorized`

**Cause**: Expired or invalid credentials

**Solution**:

```bash
# Download new credentials from your data commons
# Then refresh them by re-adding the remote
git drs remote add gen3 production \
    --cred /path/to/new-credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket
```

**Prevention**:

- Credentials expire after 30 days
- Set a reminder to refresh them regularly

---

**Error**: `Upload error: 503 Service Unavailable`

**Cause**: DRS server is temporarily unavailable or credentials expired

**Solutions**:

1. Wait and retry the operation
2. Refresh credentials:
   ```bash
   git drs remote add gen3 production \
       --cred /path/to/credentials.json \
       --url https://calypr-public.ohsu.edu \
       --project my-project \
       --bucket my-bucket
   ```
3. If persistent, download new credentials from the data commons

### Network Errors

**Error**: `net/http: TLS handshake timeout`

**Cause**: Network connectivity issues

**Solution**:

- Simply retry the command
- These are usually temporary network issues

---

**Error**: Git push timeout during large file uploads

**Cause**: Long-running operations timing out

**Solution**: Add to `~/.ssh/config`:

```
Host github.com
    TCPKeepAlive yes
    ServerAliveInterval 30
```

### File Tracking Issues

**Error**: Files not being tracked

**Symptoms**:

- Large files committed directly to Git
- `git drs ls-files` doesn't show your files

**Solution**:

```bash
# Check what's currently tracked
git drs track

# Track your file type
git drs track "*.bam"
git add .gitattributes

# Remove from Git and re-add
git rm --cached large-file.bam
git add large-file.bam
git commit -m "Track large file with git-drs"
```

---

**Error**: `[404] Object does not exist on the server`

**Symptoms**:

- After clone, git pull fails

**Solution**:

```bash
# confirm repo has complete configuration
git drs remote list

# ensure the repo is initialized
git drs init

# confirm your remote configuration and refresh auth if needed
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --project my-project \
    --organization my-program

# attempt git pull again
git drs pull
```

---

**Error**: `git drs ls-files` shows files but they won't download

**Cause**: Files may not have been properly uploaded or DRS records missing

**Solution**:

```bash
# Check repository status
git drs remote list

# Try pulling with verbose output
GIT_TRACE=1 git drs pull

# Check logs
cat .git/drs/*.log
```

### Configuration Issues

**Error**: `git drs remote list` shows empty or incomplete configuration

**Cause**: Repository not properly initialized or no remotes configured

**Solution**:

```bash
# Initialize repository if needed
git drs init

# Add Gen3 remote
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket

# Verify configuration
git drs remote list
```

---

**Error**: Configuration exists but commands fail

**Cause**: Mismatched configuration between global and local settings, or expired credentials

**Solution**:

```bash
# Check configuration
git drs remote list

# Refresh credentials by re-adding the remote
git drs remote add gen3 production \
    --cred /path/to/new-credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket
```

### Remote Configuration Issues

**Error**: `no default remote configured`

**Cause**: Repository initialized but no remotes added yet

**Solution**:

```bash
# Add your first remote (automatically becomes default)
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket
```

---

**Error**: `default remote 'X' not found`

**Cause**: Default remote was deleted or configuration is corrupted

**Solution**:

```bash
# List available remotes
git drs remote list

# Set a different remote as default
git drs remote set staging

# Or add a new remote
git drs remote add gen3 production \
    --cred /path/to/credentials.json \
    --url https://calypr-public.ohsu.edu \
    --project my-project \
    --bucket my-bucket
```

---

**Error**: Commands using wrong remote

**Cause**: Default remote is not the one you want to use

**Solution**:

```bash
# Check current default
git drs remote list

# Option 1: Change default remote
git drs remote set production

# Option 2: Specify remote for single command
git drs push staging
git drs fetch production
```

## Undoing Changes

### Untrack Files

If you accidentally tracked the wrong files:

```bash
# See current tracking
git drs track

# Remove incorrect pattern
git drs untrack "wrong-dir/**"

# Add correct pattern
git drs track "correct-dir/**"

# Stage the changes
git add .gitattributes
git commit -m "Fix tracking patterns"
```

### Undo Git Add

Remove files from staging area:

```bash
# Check what's staged
git status

# Unstage specific files
git restore --staged file1.bam file2.bam

# Unstage all files
git restore --staged .
```

### Undo Last Commit

To retry a commit with different files:

```bash
# Undo last commit, keep files in working directory
git reset --soft HEAD~1

# Or undo and unstage files
git reset HEAD~1

# Or completely undo commit and changes (BE CAREFUL!)
git reset --hard HEAD~1
```

### Remove Files from History

If you committed large files directly to Git by mistake:

```bash
# Remove from Git history (use carefully!)
git filter-branch --tree-filter 'rm -f large-file.dat' HEAD

# Then track properly with git-drs
git drs track "*.dat"
git add .gitattributes
git add large-file.dat
git commit -m "Track large file with git-drs"
```

## Diagnostic Commands

### Check System Status

```bash
# Git DRS version and help
git-drs version
git-drs --help

# Configuration
git drs remote list

# Repository status
git status
git drs ls-files
```

### View Logs

```bash
# Git DRS logs (in repository)
ls -la .git/drs/
cat .git/drs/*.log
```

### Test Connectivity

```bash

# Test DRS configuration
git drs remote list
```

## Getting Help

### Log Analysis

When reporting issues, include:

```bash
# System information
git-drs version
git --version

# Configuration
git drs remote list

# Recent logs
tail -50 .git/drs/*.log
```

## Prevention Best Practices

1. **Test in small batches** - Don't commit hundreds of files at once
2. **Verify tracking** - Always check `git drs ls-files` after adding files
3. **Use .gitignore** - Prevent accidental commits of temporary files
4. **Monitor repository size** - Keep an eye on `.git` directory size
