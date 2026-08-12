# Globus User-Driven Integration Test Plan

## Purpose

Verify, using real Globus Auth and Transfer services, that a user can materialize
a Globus collection tree as immutable DRS members, hydrate those members through
efficient multi-item tasks, and that git-drs applies access-method policy,
credential refresh, fallback, diagnostics, cache placement, and file validation
correctly.

This is a manual integration test. It is not run in CI because it requires a
browser login, real collections, and a DRS server with controlled fixtures.

## Required environment

- A build of `git-drs` from the revision under test on `PATH`.
- Git and Git LFS. The separate `globus-cli` utility is not required.
- A registered Globus native application client ID.
- A source collection on which the writer can list a dedicated test subtree,
  and a writable destination collection.
- The destination collection must expose the test repository, either at its
  root or at a configured `globus-destination-path`.
- A DRS remote and test repository prepared with the fixture objects below. The
  writer must be able to register objects with `git drs push`.
- A user authorized to discover and resolve every fixture object.

Use dedicated, non-production collections and test data. Do not display, copy,
or commit token files. Record collection IDs, task IDs, object IDs, command
output, and checksums; do not record credentials.

## Fixture data

The test administrator prepares seven small files with distinct content and
published SHA-256 checksums:

| Fixture | DRS access methods | Purpose |
| --- | --- | --- |
| `globus-only.bin` | valid Globus | successful Globus transfer |
| `https-globus.bin` | valid public HTTPS and valid Globus | selection and fallback |
| `https-only.bin` | valid public HTTPS | strict-policy diagnostic |
| `broken-globus.bin` | valid public HTTPS and a Globus URL whose source path cannot be read | execution failure and fallback boundary |
| `release/a.bin` | member of a dedicated Globus subtree | recursive writer import |
| `release/nested/b.bin` | member of the same Globus subtree | recursive listing and batching |
| `release/nested/c.bin` | member of the same Globus subtree | multi-item batching |

Commit pointers for the four standalone fixtures to the test repository. The
valid Globus URLs must use this form:

```text
globus://<source-collection-id>/<source-path>
```

Record the expected checksum and size for each file before testing. Confirm the
HTTPS and Globus copies of `https-globus.bin` contain identical bytes.

Create an authoritative tab-separated `release.tsv` for the collection subtree.
Paths are relative to `release/`; SHA-256 values must be unique:

```text
path\tsize\tsha256
a.bin\t<size>\t<sha256>
nested/b.bin\t<size>\t<sha256>
nested/c.bin\t<size>\t<sha256>
```

## Initial setup

Start from a fresh clone so existing cache entries cannot hide transfers:

```bash
git clone <test-repository-url> globus-integration
cd globus-integration
git drs remote add <remote-name> <remote-configuration-arguments>

export GIT_DRS_GLOBUS_CLIENT_ID='<native-application-client-id>'
export GIT_DRS_GLOBUS_DESTINATION_COLLECTION='<destination-collection-id>'
unset GIT_DRS_GLOBUS_TRANSFER_TOKEN
unset GIT_DRS_ACCESS_METHOD
unset GIT_DRS_TRANSFER_PROVIDER
```

If the remote is already committed or otherwise provisioned for the clone,
omit `remote add`. Keep the clone until all cases are complete.

## Test cases

### 1. Interactive login and stored credentials

1. Run `git drs auth globus logout` to remove prior git-drs Globus tokens.
2. Run `git drs auth globus login`.
3. Open the printed URL, approve access, and paste the authorization code.
4. If Globus reports required collection `data_access` scopes, repeat login
   with one `--scope '<scope>'` option per required scope.
5. Run `git drs auth globus status`.
6. Confirm `globus-cli` is not installed or is not used by any step.

Expected:

- Login reports success and the local token-storage path.
- Status reports `Globus Transfer API authentication OK`.
- The credential directory and token file are owner-only (`0700` and `0600`
  on systems that expose POSIX permissions).

### 2. Strict Globus transfer

Run:

```bash
git drs pull --include globus-only.bin --access-method globus
git drs ls-files --long globus-only.bin
shasum -a 256 globus-only.bin
```

Inspect the most recent task in the Globus web application.

Expected:

- One Globus task succeeds with label `git-drs pull`.
- Its source collection/path match the DRS `globus://` URL.
- Its destination collection is the configured user collection.
- The destination is under `/.git/lfs/objects/`.
- The worktree file is hydrated and its size and SHA-256 match the fixture.
- `git status --short` reports no content change caused by hydration.

### 3. Materialize a collection subtree at write time

In the writer repository, record `git status --short`, then run:

```bash
git drs add-url \
  globus://<source-collection-id>/release/ data/release \
  --recursive --manifest release.tsv --dry-run
git status --short

git drs add-url \
  globus://<source-collection-id>/release/ data/release \
  --recursive --manifest release.tsv --remote <remote-name>
git drs ls-files --drs
```

Expected:

- Dry-run lists three planned members in deterministic path order and changes
  no worktree, local DRS, or `.gitattributes` state.
- The real import creates three pointer files and one local DRS object per
  member. Each object has the manifest size and SHA-256 and a distinct
  `globus://<source-collection-id>/release/<member-path>` access URL.
- `.gitattributes` contains one tree rule with both
  `data/release/** filter=drs` and `data/release/** drs=ro`; it does not contain
  one rule per file.
- No source payload is downloaded into the writer worktree or LFS cache.

Stage `data/release`, `.gitattributes`, and `release.tsv`; commit them and run
`git drs push`. Preserve this publication for the batching case below.

### 4. Reject a stale or unsafe writer manifest

Add an unlisted temporary file below the live `release/` subtree and rerun the
recursive import with a new, unused destination and the unchanged manifest.

Expected: validation reports that the collection and manifest have different
members and writes no pointer, DRS object, or attribute rule.

Remove the temporary source file. In a disposable manifest, change one size,
then repeat. Expected: validation identifies a manifest mismatch and again
writes nothing. Also confirm that a manifest containing traversal such as
`../outside.bin`, malformed SHA-256, or duplicate member paths is rejected
before any repository state changes.

### 5. Batch frozen collection members efficiently

After the writer case has been pushed, use a fresh clone with Globus required:

```bash
git drs pull --include 'data/release/**' --access-method globus
```

Inspect the resulting Globus task and verify all three local checksums.

Expected:

- Exactly one `git-drs pull` task contains three explicit transfer items for
  the compatible source and destination context, rather than one task per file.
- Every source path is the path committed in its DRS object and every
  destination is the corresponding LFS cache object.
- All three worktree files are hydrated and pass local size and SHA-256 checks.

Then add another file to the live source subtree without publishing a new
manifest or DRS object, clear the three local cache entries, and repeat the
pull.

Expected: the second task still contains only the three committed members; the
new live file is neither discovered nor downloaded. Remove the temporary file
afterward.

### 6. Clone-local source routing and repository path

Unset the environment destination and configure an exact source route plus the
destination path that exposes this repository:

```bash
unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
git config --local --add \
  drs.remote.<remote-name>.globus-collection \
  '<source-collection-id>=<destination-collection-id>'
git config --local --add \
  drs.remote.<remote-name>.globus-destination-path \
  '<destination-collection-id>=<repository-path>'
git drs pull --include globus-only.bin --access-method globus
```

Expected: planning selects the exact source route, the task writes below the
configured repository path, and the checksum passes. The committed
`.git-drs/drs-policies.yaml` contains no destination collection, destination
path, or credential; those values remain clone-local.

Restore the environment destination, or remove the two local settings, before
continuing.

### 7. Deterministic automatic selection

Remove the hydrated file and its corresponding LFS cache object by using a new
fresh clone, then run:

```bash
git drs pull --include https-globus.bin
```

Expected:

- Automatic policy selects HTTPS before Globus.
- The file checksum matches.
- No new Globus transfer task is created for this file.

### 8. Preferred Globus and persistent remote preference

In a fresh clone or after clearing this fixture from the cache, run:

```bash
GIT_DRS_ACCESS_METHOD=prefer:globus \
  git drs pull --include https-globus.bin
```

Then repeat from a fresh clone using repository-local configuration:

```bash
git config --local drs.remote.<remote-name>.access-method prefer:globus
git drs pull --include https-globus.bin
```

Expected for both runs:

- A Globus task succeeds.
- The hydrated checksum matches the common HTTPS/Globus fixture checksum.

### 9. Preference precedence

With the remote still configured as `prefer:globus`, run from a fresh clone or
empty cache:

```bash
GIT_DRS_ACCESS_METHOD=prefer:https \
  git drs pull --include https-globus.bin
```

Then run again with both the environment preference and a strict CLI option:

```bash
GIT_DRS_ACCESS_METHOD=prefer:https \
  git drs pull --include https-globus.bin --access-method globus
```

Expected:

- The environment preference overrides remote configuration and uses HTTPS.
- The command option overrides the environment and uses Globus.

### 10. Missing destination configuration

Run:

```bash
unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
git drs pull --include https-globus.bin --access-method globus
```

Expected:

- Planning fails before a transfer task is submitted.
- The diagnostic identifies `globus=disabled` and names
  `GIT_DRS_GLOBUS_DESTINATION_COLLECTION`.
- The file remains a pointer and no partial cache file remains.

Restore the destination variable. Repeat with
`GIT_DRS_ACCESS_METHOD=prefer:globus` and no strict command option.

Expected: HTTPS fallback succeeds and no Globus task is submitted.

### 11. Missing Globus credential

Run `git drs auth globus logout`, ensure
`GIT_DRS_GLOBUS_TRANSFER_TOKEN` is unset, and require Globus for
`https-globus.bin`.

Expected:

- Planning fails with `globus=disabled` and login guidance.
- `GIT_DRS_ACCESS_METHOD=prefer:globus` falls back to HTTPS.
- No Globus task is submitted.

Log in again before continuing.

### 12. Rejected transfer credential

Preserve the stored login, temporarily set an invalid override, and require
Globus:

```bash
GIT_DRS_GLOBUS_TRANSFER_TOKEN='invalid-test-token' \
  git drs pull --include globus-only.bin --access-method globus
```

Expected:

- Local readiness passes because a token is configured.
- Transfer API authentication rejects the token before task submission.
- The error identifies Globus Transfer API authentication, not DRS discovery.
- No HTTPS fallback occurs after execution begins.
- Removing the override restores use of the stored credential.

### 13. No fallback after transfer starts

Run:

```bash
GIT_DRS_ACCESS_METHOD=prefer:globus \
  git drs pull --include broken-globus.bin
```

Expected:

- Globus is selected and a task is submitted.
- The task fails or becomes inactive with an actionable source-access error.
- git-drs returns that Globus failure and does not retry through HTTPS.
- The worktree remains a pointer and no invalid completed cache object remains.

Then require HTTPS for the same fixture. Expected: HTTPS succeeds, proving the
first failure tested the execution fallback boundary rather than fixture data.

### 14. Aggregate selection diagnostics

Remove the destination variable and request both `globus-only.bin` and
`https-only.bin` with strict Globus selection:

```bash
unset GIT_DRS_GLOBUS_DESTINATION_COLLECTION
git drs pull \
  --include globus-only.bin \
  --include https-only.bin \
  --access-method globus
```

Expected:

- Planning fails before any transfer begins.
- The output identifies both object IDs.
- `globus-only.bin` reports Globus as disabled.
- `https-only.bin` reports required Globus as not advertised.
- No credentials or token values appear in output.

### 15. Stored-token refresh soak test

This case is optional because it must cross a real access-token expiry. Log in
with stored refreshable credentials, leave the static token override unset, and
record the token file modification time without opening or copying its content.
Wait until the access token has expired, then run:

```bash
git drs auth globus status
git drs pull --include globus-only.bin --access-method globus
```

Expected:

- Status and transfer succeed without another browser login.
- The token file modification time advances after refresh.
- A later command succeeds using the refreshed stored credential.

Do not simulate expiry by corrupting a real credential file. Use a disposable
test token store via `GIT_DRS_GLOBUS_TOKEN_FILE` if controlled token mutation is
required.

### 16. Logout cleanup

Run:

```bash
git drs auth globus logout
git drs auth globus status
```

Expected:

- Logout reports success.
- Status fails with login guidance.
- Requiring Globus fails during planning; public HTTPS remains usable under
  `auto` or `prefer:globus`.

## Pass criteria

The integration passes when all mandatory cases meet their expected results,
every successful hydration matches its published checksum and size, no secret
appears in logs, and failures leave neither a falsely hydrated worktree file nor
a completed invalid LFS cache object. Record any skipped optional refresh case.

## Cleanup

1. Run `git drs auth globus logout` if the credentials were created solely for
   this test.
2. Delete the test clone and any disposable token file.
3. Remove transferred test objects from the destination collection if deleting
   the clone does not remove them.
4. Ask the test administrator to remove temporary DRS fixtures and source data.
5. Retain only sanitized command output, task IDs, checksums, and the tested
   git-drs revision.
