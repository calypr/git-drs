# Git LFS Compatibility

`git-drs` supports Git LFS compatibility, but Git LFS is not required for normal `git-drs` workflows.

Use this page if you are working with an older repository, a mixed environment, or a team workflow that still expects Git LFS concepts.

## What Is Optional

You do **not** need Git LFS installed in order to:

- install `git-drs`
- add a `git-drs` remote
- track files with `git drs track`
- upload with `git push` or `git drs push`
- hydrate data with `git drs pull`

## When Git LFS Still Matters

Git LFS compatibility can still be useful when:

- a repository already contains Git LFS-style pointer files
- your team already understands Git LFS tracking patterns
- you need to reason about clean/smudge filter behavior
- you are debugging interoperability with older tooling

`git-drs` uses a Git-compatible pointer workflow and fits into the same general filter architecture, but the day-to-day commands should come from `git-drs`, not from Git LFS.

## Preferred Commands

For current workflows, prefer:

| Task | Use |
|---|---|
| Track files | `git drs track "*.bam"` |
| See tracked files | `git drs ls-files` |
| Hydrate file content | `git drs pull` |
| Upload/register data | `git push` or `git drs push` |
