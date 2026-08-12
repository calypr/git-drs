# Globus User-Driven Integration Test Plan

## Purpose

Verify, using real Globus Auth and Transfer services, that a user can hydrate
DRS-backed files through Globus and that git-drs applies access-method policy,
credential refresh, fallback, diagnostics, cache placement, and file validation
correctly.

This is a manual integration test. It is not run in CI because it requires a
browser login, real collections, and a DRS server with controlled fixtures.

## Required environment

- A build of `git-drs` from the revision under test on `PATH`.
- Git and Git LFS. The separate `globus-cli` utility is not required.
- A registered Globus native application client ID.
- A readable source collection and a writable destination collection.
- The destination collection must expose the test repository, either at its
  root or at a configured `globus-destination-path`.
- A DRS remote and test repository prepared with the fixture objects below.
- A user authorized to discover and resolve every fixture object.

Use dedicated, non-production collections and test data. Do not display, copy,
or commit token files. Record collection IDs, task IDs, object IDs, command
output, and checksums; do not record credentials.

## Fixture data

The test administrator prepares four small files with distinct content and
published SHA-256 checksums:

| Fixture | DRS access methods | Purpose |
| --- | --- | --- |
| `globus-only.bin` | valid Globus | successful Globus transfer |
| `https-globus.bin` | valid public HTTPS and valid Globus | selection and fallback |
| `https-only.bin` | valid public HTTPS | strict-policy diagnostic |
| `broken-globus.bin` | valid public HTTPS and a Globus URL whose source path cannot be read | execution failure and fallback boundary |

Commit pointers for all four files to the test repository. The valid Globus
URLs must use this form:

```text
globus://<source-collection-id>/<source-path>
```

Record the expected checksum and size for each file before testing. Confirm the
HTTPS and Globus copies of `https-globus.bin` contain identical bytes.

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

### 3. Deterministic automatic selection

Remove the hydrated file and its corresponding LFS cache object by using a new
fresh clone, then run:

```bash
git drs pull --include https-globus.bin
```

Expected:

- Automatic policy selects HTTPS before Globus.
- The file checksum matches.
- No new Globus transfer task is created for this file.

### 4. Preferred Globus and persistent remote preference

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

### 5. Preference precedence

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

### 6. Missing destination configuration

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

### 7. Missing Globus credential

Run `git drs auth globus logout`, ensure
`GIT_DRS_GLOBUS_TRANSFER_TOKEN` is unset, and require Globus for
`https-globus.bin`.

Expected:

- Planning fails with `globus=disabled` and login guidance.
- `GIT_DRS_ACCESS_METHOD=prefer:globus` falls back to HTTPS.
- No Globus task is submitted.

Log in again before continuing.

### 8. Rejected transfer credential

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

### 9. No fallback after transfer starts

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

### 10. Aggregate selection diagnostics

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

### 11. Stored-token refresh soak test

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

### 12. Logout cleanup

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
