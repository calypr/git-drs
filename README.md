# Git DRS

---
# NOTICE

`git-drs` is not a pure GA4GH DRS client. It targets Syfon/Gen3-style DRS workflows and uses extensions where repo-scale behavior requires them.

---

[![Tests](https://github.com/calypr/git-drs/actions/workflows/test.yaml/badge.svg)](https://github.com/calypr/git-drs/actions/workflows/test.yaml)

**Git/DRS orchestration with Git-compatible pointer workflows**

`git-drs` manages:

- remote Gen3/Syfon configuration
- local DRS metadata
- pointer-aware push/pull orchestration
- bucket-scoped object reference workflows

## Key Features

- unified Git/data workflow around DRS-backed pointers
- Gen3/Syfon integration
- multiple remotes in one repository
- explicit file tracking and hydration
- metadata-only reference support for existing bucket objects

## How It Works

At a high level:

1. configure a remote for one `organization/project`
2. let `remote add` bootstrap repo-local `git-drs` state if needed
3. track file patterns with `git drs track`
4. add and commit with normal Git
5. remove tracked pointers with `git drs rm` when you want repository deletion to reconcile with remote DRS state
6. run `git drs push` for managed metadata registration/upload plus Git push
7. hydrate pointer files later with `git drs pull`

## Quick Start

```bash
git drs install
git drs remote add production https://example-gen3.org --provider gen3 \
  --scope HTAN_INT/BForePC --auth provider-helper:gen3-profile \
  --credential file:/path/to/credentials.json
git drs track "*.bam"
git add .gitattributes
git add sample.bam
git commit -m "Add sample"
git drs push
git drs ls-files
git drs pull -I "*.bam"
```

## Current CLI Shape

The cleaned CLI intentionally removed legacy commands:

- removed:
  - `git drs fetch`
  - `git drs list`
  - `git drs upload`
  - `git drs download`
- `git drs pull` is hydration-only
- `git drs ls-files` is the local file inventory command
- the unified `git drs remote add` takes scope through
  `--scope <organization/project>`

Example:

```bash
git drs remote add production calypr --scope HTAN_INT/BForePC \
  --credential file:/path/to/credentials.json
```

Current command split:

- `git drs push` is the managed data push path
- plain `git push` is plain Git only
- `git drs pull` hydrates tracked pointer files already present in the checkout
- `git drs ls-files` is the local tracked-file inventory command
- `git drs add-url` prepares pointer plus local metadata for existing provider objects

## Bucket Mapping Model

End users should not need to know the bucket name.

Push and pull depend on server-side bucket mapping for the requested scope. That mapping is normally provisioned once by a steward/admin using the bucket commands.

## Common Commands

| Command | Description |
| --- | --- |
| `git drs install` | Install global `git-drs` filter config |
| `git drs init` | Explicitly initialize or repair repository-local `git-drs` state |
| `git drs remote add <endpoint-or-alias>` | Add a DRS remote (operational: Calypr/Gen3 and Terra; catalog-only: Synapse and CGC) |
| `git drs preset list` | List the non-secret presets embedded in this release |
| `git drs remote list` | List configured remotes |
| `git drs remote remove <name>` | Remove a configured DRS remote |
| `git drs remote set <name>` | Set the default remote |
| `git drs track <pattern>` | Track files or globs |
| `git drs untrack <pattern>` | Stop tracking files or globs |
| `git drs rm <path>...` | Remove tracked DRS/LFS files from Git |
| `git drs ls-files` | List tracked files and localization state |
| `git drs pull` | Hydrate pointer files in the current checkout |
| `git drs push` | Register/upload objects, reconcile committed deletes, and push refs |
| `git drs add-url` | Add an existing provider object by URL or scoped key |
| `git drs add-ref` | Add a local reference to an existing DRS object |
| `git drs query` | Query a DRS object by ID |
| `git drs copy-records` | Copy Syfon records between remotes for one scope |

## Documentation

- [Documentation Home](docs/index.md)
- [Getting Started](docs/getting-started.md)
- [Commands Reference](docs/commands.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Developer Guide](docs/developer-guide.md)
- [Publishing documentation with GitHub Pages](docs/github-pages.md)
- [GA4GH DRS Scalability Gaps](docs/ga4gh-drs-scalability-gaps.md)

## Requirements

- Git
- access credentials for the target Gen3/Syfon deployment
- Go 1.26.2+ for local builds

## Support

- [GitHub Issues](https://github.com/calypr/git-drs/issues)
- [GitHub Releases](https://github.com/calypr/git-drs/releases)
