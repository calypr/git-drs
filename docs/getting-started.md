# Getting Started

This page assumes you already completed [Quick Start](quickstart.md).

Quick Start gets you running. This page explains how to think about `git-drs` once the repo is connected and usable.

## The Mental Model

Use the tools at the right layer:

- use `git` for commits, branches, merges, and `git pull`
- use `git-drs` for remote configuration, tracking rules, object hydration, upload/registration, and tracked-file delete reconciliation

The most important distinction is:

- `git pull` updates commits and checkout state
- `git drs pull` hydrates tracked pointer files already present in the checkout

## The Setup Command

The standard setup command is:

```bash
git drs remote add gen3 production <organization/project> --cred ~/.gen3/credentials.json
```

That command:

- stores the remote configuration
- imports or refreshes credentials
- bootstraps repo-local `git-drs` wiring when it is missing

## The Two Common Workflows

### Existing Repository

```bash
git clone <repo-url>
cd <repo-name>
git drs remote add gen3 production <organization/project> --cred ~/.gen3/credentials.json
git drs pull
```

### New Repository

```bash
mkdir my-data-repo
cd my-data-repo
git init
git drs remote add gen3 production <organization/project> --cred ~/.gen3/credentials.json
git drs track "*.bam"
git add .gitattributes
git commit -m "Configure tracked files"
```

## Typical Workflow

Most work reduces to this loop:

1. update Git history

   ```bash
   git pull
   ```

2. hydrate tracked files when needed

   ```bash
   git drs pull
   ```

   To hydrate only part of a repository instead of everything, use include filters:

   ```bash
   git drs pull -I "data/sample.bam"
   git drs pull -I "*.vcf.gz"
   ```

3. edit or add files normally

   ```bash
   git add ...
   git commit -m "..."
   ```

4. push data changes

   ```bash
   git drs push
   ```

`git drs push` handles the DRS upload flow and the Git push flow together.

Use plain `git push` when you only want to push Git-only changes and do not want the `git-drs` upload stage.

## The Core Tasks

### Track files

```bash
git drs track "*.bam"
git drs track "data/**"
```

Always review and stage `.gitattributes` after changing tracking rules.

### Inspect local state

```bash
git drs ls-files
git drs ls-files -l
git drs ls-files --drs
```

Interpretation:

- `*` means the worktree has localized bytes
- `-` means the worktree still has a pointer

### Remove tracked files

```bash
git drs rm sample.bam
git commit -m "Remove sample"
git drs push
```

That is the supported delete flow for tracked `git-drs` objects. For the fuller decision tree, see [Removing Files](remove-files.md).

### Refresh credentials

```bash
git drs remote add gen3 production <organization/project> --cred /path/to/new-credentials.json
```

## Read Next

- [Commands Reference](commands.md) for exact command syntax
- [Troubleshooting](troubleshooting.md) when a real workflow breaks
