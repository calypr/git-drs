# `git drs remote remove` Use Cases

This guide provides practical scenarios for removing Git DRS remotes safely.

## Adds missing `remove` verb for Git DRS remotes


To remove **Git DRS** remotes under `lfs.customtransfer.drs.remote.*` in `.git/config`.

Use `git drs remove`.  For example, to remove a remote named `foobar`:

```bash
# view a remote named 'foobar' that is no longer needed
git drs remote list
* primary    gen3     https://example.com
  foobar     gen3     https://example.com
git drs remote rm foobar
# or: git drs remote remove foobar
git drs remote list
* primary    gen3     https://example.com
```

Expected outcome:
- `foobar` is removed from `git drs remote list`.
- Related `lfs.customtransfer.drs.remote.foobar.*` entries are removed from config.
- If `foobar` was the default, another remote becomes default automatically (or default is cleared if none remain).

## When to use `git drs remote remove`

Use this command when a remote configuration is no longer valid or no longer needed.

```bash
git drs remote remove <remote-name>
```

## Use Case 1: Retire a staging environment

A temporary staging DRS server is decommissioned after release.

```bash
git drs remote list
git drs remote remove staging
git drs remote list
```

Expected outcome:
- `staging` no longer appears in `git drs remote list`.
- Remaining remotes continue to work.

## Use Case 2: Remove and replace a misconfigured remote

A remote was created with the wrong endpoint, bucket, or project.

```bash
git drs remote remove production
git drs remote add gen3 production \
  --url https://correct.example.org \
  --cred /path/to/credentials.json \
  --project correct-project \
  --bucket correct-bucket
```

Expected outcome:
- Old configuration is removed.
- New remote can be added under the same name.

## Use Case 3: Remove current default remote

If you remove the default remote, Git DRS automatically assigns another existing remote as default.

```bash
git drs remote list
git drs remote remove origin
git drs remote list
```

Expected outcome:
- A remaining remote is marked with `*` as the new default.
- If no remotes remain, there is no default remote.

## Use Case 4: Clean up local test remotes

During development, test remotes can accumulate and should be deleted after validation.

```bash
git drs remote remove test-local
git drs remote remove test-ci
```

Expected outcome:
- Configuration is simplified.
- Team members avoid accidentally targeting stale remotes.
