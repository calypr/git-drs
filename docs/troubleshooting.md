# Troubleshooting

Common issues and solutions when working with Git DRS.

## When to Use Which Tool

Understanding when to use Git, Git LFS, or Git DRS commands:

### Git DRS Commands
**Use for**: Repository initialization and configuration
- `git drs init` - Set up credentials and server configuration
- `git drs list-config` - Check configuration
- `git drs add-url` - Add S3 file references

**When**: 
- Setting up a new repository
- Refreshing expired credentials
- Adding external file references

### Git LFS Commands
**Use for**: File tracking and management
- `git lfs track` - Define which files to track
- `git lfs ls-files` - See tracked files and status
- `git lfs pull` - Download specific files
- `git lfs untrack` - Stop tracking file patterns

**When**:
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

### Authentication Errors

**Error**: `Upload error: 403 Forbidden` or `401 Unauthorized`

**Cause**: Expired or invalid credentials

**Solution**:
```bash
# Download new credentials from your data commons
# Then refresh them
git drs init --cred /path/to/new-credentials.json
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
   git drs init --cred /path/to/credentials.json
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

**Error**: Files not being tracked by LFS

**Symptoms**: 
- Large files committed directly to Git
- `git lfs ls-files` doesn't show your files

**Solution**:
```bash
# Check what's currently tracked
git lfs track

# Track your file type
git lfs track "*.bam"
git add .gitattributes

# Remove from Git and re-add
git rm --cached large-file.bam
git add large-file.bam
git commit -m "Track large file with LFS"
```

---

**Error**: `[404] Object does not exist on the server`

**Symptoms**: 
- After clone, git pull fails

**Solution**:
```bash
# confirm repo has complete configuration
git drs list-config

# init your git drs project
git drs init --cred /path/to/cred/file

# attempt git pull again
git lfs pull -I path/to/file
```

---

**Error**: `git lfs ls-files` shows files but they won't download

**Cause**: Files may not have been properly uploaded or DRS records missing

**Solution**:
```bash
# Check repository status
git drs list-config

# Try pulling with verbose output
git lfs pull -I "problematic-file*" --verbose

# Check logs
cat .drs/*.log
```

### Configuration Issues

**Error**: `git drs list-config` shows empty or incomplete configuration

**Cause**: Repository not properly initialized

**Solution**:
```bash
# For existing Gen3 setup
git drs init --cred /path/to/credentials.json

# For new Gen3 setup
git drs init --profile <name> \
             --url <server-url> \
             --cred <creds-file> \
             --project <project-id> \
             --bucket <bucket-name>

# For AnVIL
git drs init --server anvil --terraProject <project-id>
```

---

**Error**: Configuration exists but commands fail

**Cause**: Mismatched configuration between global and local settings

**Solution**:
```bash
# Check both configurations
cat ~/.drs/config
cat .drs/config

# Re-initialize if needed
git drs init --cred /path/to/credentials.json
```

## Undoing Changes

### Untrack LFS Files

If you accidentally tracked the wrong files:

```bash
# See current tracking
git lfs track

# Remove incorrect pattern
git lfs untrack "wrong-dir/**"

# Add correct pattern
git lfs track "correct-dir/**"

# Stage the changes
git add .gitattributes
git commit -m "Fix LFS tracking patterns"
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

### Remove Files from LFS History

If you committed large files directly to Git by mistake:

```bash
# Remove from Git history (use carefully!)
git filter-branch --tree-filter 'rm -f large-file.dat' HEAD

# Then track properly with LFS
git lfs track "*.dat"
git add .gitattributes
git add large-file.dat
git commit -m "Track large file with LFS"
```

## Diagnostic Commands

### Check System Status

```bash
# Git DRS version and help
git-drs version
git-drs --help

# Configuration
git drs list-config

# Repository status
git status
git lfs ls-files
```

### View Logs

```bash
# Git DRS logs (in repository)
ls -la .drs/
cat .drs/*.log
```

### Test Connectivity

```bash
# Test basic Git operations
git lfs pull --dry-run

# Test DRS configuration
git drs list-config
```

## Getting Help

### Log Analysis

When reporting issues, include:

```bash
# System information
git-drs version
git lfs version
git --version

# Configuration
git drs list-config

# Recent logs
tail -50 .drs/*.log
```

## Prevention Best Practices

1. **Test in small batches** - Don't commit hundreds of files at once
2. **Verify tracking** - Always check `git lfs ls-files` after adding files
3. **Use .gitignore** - Prevent accidental commits of temporary files
4. **Monitor repository size** - Keep an eye on `.git` directory size
