# Globus user integration test runner

This directory turns
[`../globus-user-integration-test-plan.md`](../globus-user-integration-test-plan.md)
into user-driven checks against real Globus Auth, Globus Transfer, and DRS
services. It does not run in CI and does not require `globus-cli`.

## Safety model

Use only dedicated non-production collections and repositories. Scripts never
print tokens. Each reader case uses a new clone and leaves it under `WORK_ROOT`
for inspection. The writer defaults to dry-run; materialization and publication
require separate opt-ins. Source-collection mutation, task inspection, token
expiry, and cleanup remain manual because automating them would require broader
collection permissions or unsafe credential handling.

## Get the three Globus IDs

All three values look like UUIDs, for example
`12345678-1234-1234-1234-123456789abc`, but they identify different things.
They are identifiers, not access tokens or passwords.

### `SOURCE_COLLECTION_ID`: where the test files come from

This is the ID of the Globus collection containing `release/` and the other
source files.

#### Create a source collection on your computer

For one-person testing, the simplest option is
[Globus Connect Personal](https://docs.globus.org/globus-connect-personal/install/).
It turns a Mac, Windows, or Linux computer into a Globus collection; you do not
need to install Globus Connect Server.

1. Download and install Globus Connect Personal for your operating system.
2. Start it, sign in with the same Globus identity you will use for testing,
   and approve the requested consent.
3. During setup, give the collection a recognizable name such as
   `git-drs source test`.
4. Add the local directory that will hold the fixtures to Globus Connect
   Personal's accessible folders/directories. Read access is sufficient for
   the source; write access is needed while copying the fixtures into it. See note (1) below.
5. Keep Globus Connect Personal running whenever a test lists or transfers
   files from this collection.
6. Copy this repository's `fixtures/release/` directory into the accessible
   source directory at the path configured by `SOURCE_RELEASE_PATH`. With the
   default `.env.example`, the collection should show:

   ```text
   /release/a.bin
   /release/nested/b.bin
   /release/nested/c.bin
   ```

Setup creates the collection automatically. Open it in Globus File Manager and
confirm that you can browse those three files before running `writer.sh`.

If the source data lives on a shared server, cluster, or institutional storage,
do not install Globus Connect Personal there. Ask the storage administrator for
an existing readable collection or for a Globus Connect Server collection;
server collection creation normally requires administrator privileges. The
official [collections and endpoints overview](https://docs.globus.org/guides/overviews/collections-and-endpoints/)
explains the difference.

#### Note (1):
The fixture files and their contents are already committed under:

[fixtures](/Users/walsbr/calypr/git-drs/tests/globus-user-integration-test/fixtures)

The user runs:

```bash
cd tests/globus-user-integration-test
scripts/prepare-fixtures.sh
```

That script generates:

- `fixtures/release.tsv` with each file’s path, size, and SHA-256.
- `fixtures/checksums.env` with expected checksums.

It does not upload files into Globus. The user must copy the existing `fixtures/release/` directory into the source collection’s accessible directory, for example:

```bash
cp -R fixtures/release /path/exposed/by/globus/
```

The standalone fixture files are also provided, but publishing their DRS records remains an administrator/manual setup step.

#### Copy its ID

1. Sign in to the [Globus Web App](https://app.globus.org/).
2. Open **File Manager** and search for the collection holding the source
   fixtures.
3. Open the collection and copy its collection ID from its details or URL.
4. Confirm that you can browse the collection and see `SOURCE_RELEASE_PATH`.

If another person manages the source collection, ask that administrator for
the collection UUID. Do not use a file path, collection display name, storage
gateway ID, or user identity ID. Globus defines the collection `id` as the
collection's unique UUID; its collection browser also returns this value as an
`endpoint_id` for historical API compatibility. See the official
[collection API](https://docs.globus.org/globus-connect-server/v5/api/openapi_Collections/)
and [collection browser documentation](https://docs.globus.org/api/helper-pages/browse-collections/).

### `GIT_DRS_GLOBUS_CLIENT_ID`: which application is logging in

This is the Globus Auth client ID for git-drs's interactive login. It is not a
collection ID and it is not a client secret.

1. Open [Globus developer settings](https://app.globus.org/settings/developers)
   and sign in.
2. Create or select a developer project.
3. Register a **Thick Client** application, because git-drs is installed and
   runs on the user's computer. Give it a recognizable name such as
   `git-drs integration test`.
4. Open the registered application and copy its **Client ID** into
   `GIT_DRS_GLOBUS_CLIENT_ID`.

The native/thick client does not need a client secret in `.env`. During
`git drs auth globus login`, the client ID identifies the application while
the user signs in and grants consent. The official
[Globus Auth application-registration guide](https://docs.globus.org/api/auth/developer-guide/#register-app)
describes this flow.

If your organization already registered an approved git-drs native client,
use the client ID supplied by its administrator instead of creating another.

### `GIT_DRS_GLOBUS_DESTINATION_COLLECTION`: where downloads land

This is the ID of a collection that can write into the machine or filesystem
containing `WORK_ROOT`. It is usually your Globus Connect Personal collection
or an institutional collection that exposes the test workspace.

1. In the Globus Web App, open **File Manager**.
2. Search for the collection connected to the filesystem where the disposable
   clones will live.
3. Open it and copy its collection ID from its details or URL.
4. Verify that you can create files there and that `WORK_ROOT` is inside the
   collection's exposed filesystem tree.

The source and destination may use the same collection, but normally they are
different.

### `GLOBUS_DESTINATION_ROOT_PATH`: the Globus view of `WORK_ROOT`

`WORK_ROOT` and `GLOBUS_DESTINATION_ROOT_PATH` name the same directory from two
different viewpoints:

- `WORK_ROOT` is the absolute path used by scripts on the local computer.
- `GLOBUS_DESTINATION_ROOT_PATH` is the path shown for that directory in
  Globus File Manager. It starts with `/` and is relative to the destination
  collection's root.

The scripts create disposable clones directly below `WORK_ROOT`. They use
`GLOBUS_DESTINATION_ROOT_PATH` to tell Globus where those same clones and their
`.git/lfs/objects` directories appear inside the collection.

Example: if a Globus Connect Personal collection exposes the local home
directory as its root:

```dotenv
WORK_ROOT=/Users/alice/globus-tests
GLOBUS_DESTINATION_ROOT_PATH=/globus-tests
```

Example: if an institutional collection maps local `/data/projects` to
collection root `/`, then:

```dotenv
WORK_ROOT=/data/projects/git-drs-tests
GLOBUS_DESTINATION_ROOT_PATH=/git-drs-tests
```

To find the value, create `WORK_ROOT`, open the destination collection in
Globus File Manager, browse to that same directory, and copy the path shown in
the **Path** field. Do not put a collection UUID or local filesystem path in
`GLOBUS_DESTINATION_ROOT_PATH`. If you cannot browse to `WORK_ROOT`, expose it
through Globus Connect Personal's accessible directories or ask the collection
administrator for the correct local-to-collection path mapping.

## Prepare

1. Copy `.env.example` to `.env` and fill every required value.
2. Run `scripts/prepare-fixtures.sh`.
3. Upload `fixtures/release/` to `SOURCE_RELEASE_PATH` in the source collection.
   This means copying the provided files into the directory exposed by Globus;
   with Globus Connect Personal it is an ordinary local copy.
4. Copy `fixtures/standalone/globus-only.bin` and
   `fixtures/standalone/https-globus.bin` to `SOURCE_STANDALONE_PATH` in the
   source collection. Host the three HTTPS fixtures at
   `STANDALONE_HTTPS_BASE_URL`; each URL is that base plus the filename. Do not
   put `broken-globus.bin` at its advertised broken Globus path.
5. Ensure `WORK_ROOT` is a direct child of the filesystem root exported by the
   destination collection. Set `GLOBUS_DESTINATION_ROOT_PATH` to the matching
   collection path. For example, if the collection exports `/data`,
   `WORK_ROOT=/data/globus-tests` pairs with
   `GLOBUS_DESTINATION_ROOT_PATH=/globus-tests`.
6. Make scripts executable if the checkout did not preserve modes:

   ```bash
   chmod +x scripts/*.sh
   ```

The generated `fixtures/release.tsv`, `fixtures/checksums.env`, and `.env` are
ignored by Git. Compare or copy the generated checksum values into `.env` when
the server fixtures do not use the included bytes exactly.

## Authenticate and preflight

```bash
scripts/auth.sh logout
scripts/auth.sh login
scripts/auth.sh status
scripts/preflight.sh
```

Pass required collection `data_access` scopes directly to `git drs auth globus
login --scope ...` if consent is required; `auth.sh` intentionally keeps the
interactive wrapper simple.

## Writer cases

```bash
scripts/writer.sh --dry-run

> NOTE: set ALLOW_DRS_PUSH ALLOW_WRITER_MUTATION in the .env file!!

# Creates pointers only in a disposable clone.
ALLOW_WRITER_MUTATION=yes scripts/writer.sh --apply

# Commits, registers, uploads Git state, and pushes. Review the dry-run first.
ALLOW_WRITER_MUTATION=yes ALLOW_DRS_PUSH=yes scripts/writer.sh --publish

# Rejects traversal, malformed SHA-256, and duplicate member paths.
scripts/writer-negative.sh
```

After dry-run, manually exercise the stale-size and unlisted-live-file cases
described in plan case 4; those require changing the real source collection.

## Publish standalone fixtures

This creates the four specialized DRS objects required by the reader cases:

```bash
# Creates pointers and local DRS metadata in a disposable clone.
ALLOW_WRITER_MUTATION=yes scripts/publish-standalone.sh --apply

# After inspection, commits and registers them through git drs push.
ALLOW_WRITER_MUTATION=yes ALLOW_DRS_PUSH=yes \
  scripts/publish-standalone.sh --publish
```

The script calculates size and SHA-256 from `fixtures/standalone/` and creates:

| Pointer | Access methods |
| --- | --- |
| `globus-only.bin` | working Globus |
| `https-globus.bin` | working HTTPS and Globus |
| `https-only.bin` | working HTTPS |
| `broken-globus.bin` | working HTTPS and intentionally missing Globus path |

Before materializing the fixtures, the script configures the disposable clone
with the equivalent of:

```bash
git drs remote add "$TEST_REMOTE" "$TEST_DRS_ENDPOINT" \
  --provider "$TEST_DRS_PROVIDER" \
  --auth "$TEST_DRS_AUTH" \
  --scope "$TEST_DRS_SCOPE" \
  --credential "$TEST_DRS_CREDENTIAL"
```

Set these values in `.env`. `TEST_DRS_CREDENTIAL` is a credential locator such
as `env:GIT_DRS_TOKEN`, never the credential itself. It may be empty for a
public server. `TEST_DRS_STORAGE` is also optional and is passed as `--storage`
when the server requires an explicit publishing bucket or prefix.

`--apply` configures the clone but does not commit, push, or contact the DRS
server. `--publish` commits and runs `git drs push`; it requires both safety
opt-ins and working credentials for the configured remote.

`TEST_REMOTE` is the name of the git-drs remote configuration used by the integration tests.

Example:

```dotenv
TEST_REMOTE=research
```

This is only a local label. It is not:

- a Git repository URL;
- a DRS endpoint URL;
- a Globus collection ID;
- a hostname.

A complete git-drs remote named `research` contains information such as:

```text
Name:      research
Endpoint:  https://drs.example.org
Provider:  gen3
Scope:     organization/project
Auth:      bearer
```

The scripts use `TEST_REMOTE` in commands such as:

```bash
git drs add-url ... --remote "$TEST_REMOTE"
git drs pull "$TEST_REMOTE"
git drs push "$TEST_REMOTE"
```

They also make it the default inside disposable clones:

```bash
git config --local drs.default-remote "$TEST_REMOTE"
```

Setting only:

```dotenv
TEST_REMOTE=research
```

does not create the remote configuration. It merely tells the scripts which existing configuration to use.

The publisher configures the remote in its disposable clone from `.env`. The
equivalent manual command is:

```bash
git drs remote add "$TEST_REMOTE" "$TEST_DRS_ENDPOINT" \
  --provider "$TEST_DRS_PROVIDER" \
  --auth "$TEST_DRS_AUTH" \
  --scope "$TEST_DRS_SCOPE" \
  --credential "$TEST_DRS_CREDENTIAL"
```

The precise options depend on your DRS server and authentication method.

Alternatively, commit a shared `.git-drs/drs-policies.yaml` containing the non-secret remote endpoint and policy. Every clone can then discover `research`, while credentials remain local.

Check the configuration with:

```bash
git drs remote list
git config --local --get drs.default-remote
```

For `--publish`, the configured remote must be writable because
`git drs push "$TEST_REMOTE"` registers the fixture objects.

### Run `materialize-standalone` directly

Normally, use `scripts/publish-standalone.sh`; it creates a disposable clone,
loads `.env`, runs the helper, and optionally publishes the result. Run the Go
helper directly only when debugging or when you already have a target clone.

From the root of the git-drs source checkout:

```bash
go run ./tests/globus-user-integration-test/cmd/materialize-standalone \
  --repo /absolute/path/to/test-repository \
  --fixtures ./tests/globus-user-integration-test/fixtures/standalone \
  --source-collection "$SOURCE_COLLECTION_ID" \
  --source-path "$SOURCE_STANDALONE_PATH" \
  --https-base "$STANDALONE_HTTPS_BASE_URL"
```

Flags:

| Flag | Meaning |
| --- | --- |
| `--repo` | Existing Git clone in which pointers and local DRS metadata are written |
| `--fixtures` | Directory containing the four standalone fixture files |
| `--source-collection` | UUID used in the generated `globus://` URLs |
| `--source-path` | Directory inside that collection, normally `/standalone` |
| `--https-base` | Public URL prefix; the helper appends each HTTPS fixture filename |

The helper calculates each file's size and SHA-256, writes four pointer files at
the target repository root, writes local DRS records below
`.git/drs/lfs/objects/`, and adds exact read-only rules to `.gitattributes`. It
prints one line per fixture containing its name, size, and SHA-256.

It does not clone, commit, push, upload fixture bytes, verify that the HTTPS URLs
work, or verify that the Globus source paths exist. To publish after a direct
run, inspect the generated state and use the normal repository workflow:

```bash
cd /absolute/path/to/test-repository
git drs ls-files --long
git add globus-only.bin https-globus.bin https-only.bin broken-globus.bin \
  .gitattributes
git commit -m "test: publish standalone Globus fixtures"
git drs push <remote-name>
```

Run the helper only in a clean, disposable clone: existing pointer or local DRS
records with the same fixture hashes may be overwritten.

## Reader cases

Run the safe cases individually so Globus task history remains easy to inspect:

```bash
scripts/reader.sh strict
scripts/reader.sh auto
scripts/reader.sh prefer
scripts/reader.sh precedence
scripts/reader.sh batch
scripts/reader.sh routing
scripts/reader.sh fallback
scripts/reader.sh diagnostics
scripts/reader.sh rejected
scripts/reader.sh broken
```

Each script verifies local outcomes and prints a `CHECKPOINT` for the task-level
assertions that must be checked in the Globus web application. For the frozen
member test, add an unpublished fourth source file, rerun `batch`, and confirm
the task still contains only the three committed members.

Cases requiring logout, a real token-expiry wait, source mutation, or an
alternate HTTPS run remain explicit manual steps in the parent plan.

## Cleanup

Review each printed clone path, then remove those disposable clones and any
transferred cache objects using your normal collection administration tools.
Run `scripts/auth.sh logout` only if the credentials were created solely for
this test. Never retain `.env`, token files, or unsanitized authentication logs.
