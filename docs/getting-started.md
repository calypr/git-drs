# Getting Started

This guide walks through the current `git-drs` workflow on the cleaned CLI path.

> **Navigation:** [Installation](installation.md) -> **Getting Started** -> [Commands Reference](commands.md) -> [Troubleshooting](troubleshooting.md)

## What `git-drs` Does

`git-drs` manages:

- Git-compatible pointer files
- local DRS metadata
- remote Syfon/Gen3 configuration
- pointer hydration and object registration workflows

It no longer tries to be a mixed bag of Git, Git LFS, and DRS transport wrappers.

## Cloning an Existing Repository

1. Clone the repository:

   ```bash
   git clone <repo-clone-url>.git
   cd <name-of-repo>
   ```

2. If you use SSH remotes, make sure your SSH setup is already working for that host.

   A typical keepalive configuration looks like:

   ```
   Host github.com
       TCPKeepAlive yes
       ServerAliveInterval 30
   ```

3. If the repo does not yet have a `git-drs` remote configured, add one now. `remote add` will bootstrap the repo-local hooks/config automatically if needed.

4. Hydrate tracked files if needed:

   ```bash
   git drs pull
   ```

This is the normal onboarding flow for an existing repo. `git drs pull` hydrates pointer files already present in the checkout. It does not replace `git pull`.

## One-Time Machine Setup

Install `git-drs` and the global Git filter configuration:

```bash
git drs install
```

## One-Time Repository Setup

After cloning or creating a repository, the normal first step is adding a remote:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/credentials.json
```

That command now sets up repository-local `git-drs` state and hooks automatically if they are missing.

You can still run `git drs init` directly when you want to initialize the repo explicitly before configuring any remote, or when you want to repair the hook/config wiring.

## Add a Gen3 Remote

The current shape is:

```bash
git drs remote add gen3 [remote-name] <organization/project> [--cred <file> | --token <token>]
```

Example:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/credentials.json
```

Notes:

- scope is one positional argument: `organization/project`
- if repo-local `git-drs` setup is missing, this command initializes it first
- users do not provide `--bucket`
- users do not provide `--url`
- bucket resolution is scope-based and server-backed

Verify:

```bash
git drs remote list
```

## New Repository Setup

For a new repository or a repository that has not yet been configured with `git-drs`:

1. Initialize the repository:

   ```bash
   git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/credentials.json
   ```

   This bootstraps the repo-local `git-drs` hooks/config if needed.

2. Verify the configuration:

   ```bash
   git drs remote list
   ```

## Steward/Admin Prerequisite

Push and pull depend on server-side bucket mapping for the target scope.

That usually means a steward/admin has already done something like:

```bash
git drs bucket add production \
  --bucket cbds \
  --region us-east-1 \
  --access-key "$AWS_ACCESS_KEY_ID" \
  --secret-key "$AWS_SECRET_ACCESS_KEY"

git drs bucket add-organization production \
  --organization HTAN_INT \
  --path s3://cbds/htan-int

git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/htan-int/bforepc
```

End users generally should not need to know the bucket name.

## Credentials

For Gen3-backed deployments:

- obtain a credential JSON or token from the target data commons
- the common path is: log in -> profile -> create API key -> download JSON
- refresh it when it expires
- re-run `git drs remote add gen3 ... --cred ...` when you need to refresh the stored profile

Example:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/new-credentials.json
```

### When a key expires or is replaced

The supported recovery path is:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/new-credentials.json
```

Practical answers to the common questions:

- do you need to run `git drs init` again?
  - no
- do you need to run `git drs remote add gen3` again?
  - yes, if the API key itself was replaced or you want to import a new credential file/token
- does `git-drs` detect token expiry automatically?
  - partially
  - if the stored access token is expired but the stored API key is still valid, `git-drs` will try to refresh the access token automatically
  - if the API key itself is expired, revoked, or replaced, rerun `git drs remote add gen3 ...`
- how do you check what remote/profile is in use?
  - `git drs remote list` shows the configured remotes
  - the Gen3 profile data lives in `~/.gen3/gen3_client_config.ini`

As a rule, if credentials changed and you want a predictable fix, re-run `git drs remote add gen3 ...` for that remote. That updates the stored profile and repo token plumbing without requiring repo re-initialization.

## Managing Additional Remotes

You can add multiple remotes for multi-environment workflows.

```bash
git drs remote add gen3 staging HTAN_INT/BForePC --cred /path/to/staging-credentials.json
git drs remote list
git drs remote set staging
```

Or target a non-default remote for a single command:

```bash
git drs push production
git drs copy-records staging production HTAN_INT/BForePC
```

## Track Files

Track file types or paths you want managed by `git-drs`:

```bash
git drs track "*.bam"
git add .gitattributes
```

You can also track explicit paths or path globs:

```bash
git drs track "data/**"
git add .gitattributes
```

View current tracking:

```bash
git drs track
```

Stop tracking patterns:

```bash
git drs untrack "*.bam"
git add .gitattributes
```

## Add, Commit, and Push

```bash
git add sample.bam
git commit -m "Add sample"
git drs push
```

`git-drs` handles pointer/object registration behavior around the Git workflow.

## Remove a Tracked File

Use `git drs rm` for tracked DRS/LFS files:

```bash
git drs rm sample.bam
git commit -m "Remove sample"
git drs push
```

This removes the pointer from Git immediately. The remote DRS mutation happens only when that deletion is committed and pushed:

- if the scoped record has one `controlled_access` entry, the record is deleted
- if it has multiple `controlled_access` entries, only the current `organization/project` resource is removed
- underlying object bytes are not deleted by default

## Inspect Tracked Files

Use `ls-files` as the local inventory command:

```bash
git drs ls-files
git drs ls-files -l
git drs ls-files --drs
git drs ls-files -I "*.bam"
```

Interpretation:

- `*` means localized/hydrated in the worktree
- `-` means the worktree still contains a pointer

## Hydrate Files

Use `git drs pull` only for hydration.

```bash
git drs pull
git drs pull -I "*.bam"
git drs pull -I "results/**" -I "*.txt"
```

Important:

- `git drs pull` does not run `git pull`
- run plain `git pull` yourself when you want new commits/trees
- then run `git drs pull` if you need to hydrate pointer files in the checkout

## Add Existing Bucket Objects

If the object already exists in provider storage, use `add-url`:

```bash
# Track the file pattern first
git drs track "myfile.txt"
git add .gitattributes

# Add object reference (known sha256 path)
git drs add-url s3://bucket/path/to/file myfile.txt \
  --sha256 <file-hash>

# Or use unknown-sha
git drs add-url s3://bucket/path/to/file myfile.txt

# Commit and push
git add myfile.txt
git commit -m "Add S3 file reference"
git push
```

Scoped bucket-key mode also works:

```bash
git drs add-url path/to/object.bin data/from-bucket.bin --scheme s3
git commit -m "Add bucket-backed object reference"
git push
```

Explicit provider URL mode also works:

```bash
git drs add-url s3://my-bucket/path/to/object.bin data/from-bucket.bin
```

## Session Workflow

> **Note:** You do not need to run `git drs init` again. Remote configuration bootstraps repo-local setup when needed, and explicit `git drs init` is mainly for manual setup or repair.

For a normal work session:

1. Refresh credentials if needed

   ```bash
   git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/new-credentials.json
   ```

   You do not need to run `git drs init` again for this. Refreshing credentials is a `remote add` operation, not a repo reinitialization step.

2. Update Git history if needed

   ```bash
   git pull
   ```

3. Hydrate tracked files if needed

   ```bash
   git drs pull
   ```

4. Work with files normally

   ```bash
   git add ...
   git commit -m "..."
   git push
   ```

## Configuration Management

View current remote configuration:

```bash
git drs remote list
```

Refresh or update credentials by re-adding the remote:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/new-credentials.json
```

## Local DRS Server Setup

Use this flow when developing against a local Syfon/DRS server instead of a hosted Gen3 deployment.

1. Add the local remote:

   ```bash
   git drs remote add local origin http://localhost:8080 \
       calypr/end_to_end_test \
       --username drs-user \
       --password drs-pass
   ```

   This bootstraps the repo-local `git-drs` hooks/config if needed.

   If your local server requires basic auth, include the local auth flags supported by that command.

2. Track and push:

   ```bash
   git drs track "*.bin"
   git add .gitattributes data/example.bin
   git commit -m "Add local DRS test file"
   git drs push
   ```

3. Verify hydration:

   ```bash
   git drs pull
   ```

For full local/remote runbooks, see [E2E Modes + Local Setup](e2e-modes-and-local-setup.md).

## Copy Metadata Between Remotes

Use `copy-records` to copy Syfon metadata records between remotes for a single scope:

```bash
git drs copy-records dev prod HTAN_INT/BForePC
```

Or let the default remote be the source:

```bash
git drs copy-records prod HTAN_INT/BForePC
```

This copies metadata only. It does not copy object bytes between buckets.

## Common Flow Summary

```bash
git drs install
git drs remote add gen3 production HTAN_INT/BForePC --cred /path/to/credentials.json
git drs track "*.bam"
git add .gitattributes
git add sample.bam
git commit -m "Add sample"
git push
git drs ls-files
git drs pull -I "*.bam"
```

For command details, see [commands.md](commands.md).
