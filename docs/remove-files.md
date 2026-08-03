# Removing Files

There are two different questions when you remove a file from a `git-drs` repository:

1. Do you just want to remove the path from Git?
2. Do you also want the pushed deletion to reconcile remote DRS state for that object?

For tracked `git-drs` files, the recommended command is `git drs rm`.

## Which Command To Use

### Use `git drs rm` for tracked `git-drs` files

```bash
git drs rm DATA/subject-123/vcf/sample1.vcf.gz
```

Use this when you want the supported Git-DRS delete workflow.

What it does immediately:

- validates that the path is a tracked `git-drs` file
- removes the path from the worktree and index
- stages the deletion through normal Git

What happens later, when the deletion is committed and pushed:

- `git-drs` derives deleted pointers from the pushed Git commit delta
- if the object is still live somewhere else in the pushed repo state, only the local path deletion is reconciled
- if the scoped record has exactly one `controlled_access` resource, the remote record is deleted
- if the record has multiple `controlled_access` resources, only the current `organization/project` resource is removed

### Use `git rm` for ordinary Git-managed files

```bash
git rm README.md
```

Use this for files that are not tracked by `git-drs`.

## Typical Tracked-File Removal Flow

```bash
git drs rm DATA/subject-123/vcf/sample1.vcf.gz
git commit -m "Remove sample"
git drs push
```

That is the supported tracked-object delete flow.

## Best Practice

For data objects managed by `git-drs`, prefer:

```bash
git drs rm <path>
git commit -m "Remove tracked object"
git drs push
```
