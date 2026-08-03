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

## Connect A Remote

List and inspect the presets shipped with the current release:

```bash
git drs preset list
git drs preset show calypr
```

Then add a named remote from a preset. For example, a scoped Calypr/Gen3
remote using a credential file is:

```bash
git drs remote add production calypr --scope <organization/project> \
  --credential file:~/.gen3/credentials.json
```

This command:

- expands the preset into a pinned endpoint, provider, authentication method,
  and preset catalog version
- stores only the credential source, not an inline secret
- bootstraps repo-local `git-drs` wiring when it is missing

The built-in presets are `calypr`, `terra`, `synapse`, and `cgc`. Calypr/Gen3
and Terra have operational runtime adapters; Synapse and CGC are catalog-only
and execution commands reject them until their adapters are implemented. The local
remote name is optional: `git drs remote add calypr ...` derives the name
`calypr`. You can also connect an unlisted HTTPS endpoint directly:

```bash
git drs remote add research https://drs.example.org \
  --provider ga4gh --auth none
```

## The Two Common Workflows

### Existing Repository

```bash
git clone <repo-url>
cd <repo-name>
git drs remote add production calypr --scope <organization/project> \
  --credential file:~/.gen3/credentials.json
git drs pull
```

### New Repository

```bash
mkdir my-data-repo
cd my-data-repo
git init
git drs remote add production calypr --scope <organization/project> \
  --credential file:~/.gen3/credentials.json
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

Use plain `git push` when you only want Git ref updates and do not want the `git-drs` registration/upload stage.

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

### Change a credential source

```bash
git drs remote remove production
git drs remote add production calypr --scope <organization/project> \
  --credential file:/path/to/new-credentials.json
```

The unified command refuses to overwrite an existing remote. Remove and add it
again when its endpoint, preset, or credential source must change. Prefer a
refreshing helper or profile source when the provider supports one.

## Read Next

- [Commands Reference](commands.md) for exact command syntax
- [Troubleshooting](troubleshooting.md) when a real workflow breaks
