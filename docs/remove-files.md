# Removing Files

There are three different questions when you remove a file from a `git-drs` repository:

1. Do you just want to remove the path from Git?
2. Do you want to delete its remote DRS metadata record?
3. Do you want to delete its stored payload?

These are separate operations. For tracked `git-drs` paths, use `git drs rm`.

## Which Command To Use

### Use `git drs rm` for tracked `git-drs` files

```bash
git drs rm DATA/subject-123/vcf/sample1.vcf.gz
```

Use this when you want to remove a tracked path from Git.

What it does immediately:

- validates that the path is a tracked `git-drs` file
- removes the path from the worktree and index
- stages the deletion through normal Git

What happens later, when the deletion is committed and pushed:

- the pointer is removed from the pushed Git tip
- `git drs push` does not delete the remote DRS record or stored payload
- historical Git commits can continue to refer to the DRS object

Use `git drs delete` when you explicitly intend to delete a DRS metadata record. Stored-payload cleanup is handled by the storage audit and retention system, not by `git drs rm` or normal push.

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

That is the supported tracked-path removal flow. The remote DRS record and stored payload remain unchanged.

## Best Practice

For data objects managed by `git-drs`, prefer:

```bash
git drs rm <path>
git commit -m "Remove tracked object"
git drs push
```

Run a separate explicit metadata deletion only when the record should no longer exist. Do not rely on normal push to infer remote retention from one Git ref update.
