# Globus tutorial integration test

This user-driven test creates DRS pointer files for the three public files in
[Globus Tutorial Collection 1](https://docs.globus.org/guides/tutorials/manage-files/transfer-files/),
registers their DRS records in a local Syfon server, downloads them with one
Globus task, and verifies their SHA-256 checksums. It does not require
`globus-cli` or a private source collection.

## What you must provide

| Setting | What it is | Secret? |
| --- | --- | --- |
| `GIT_DRS_GLOBUS_CLIENT_ID` | UUID of a Globus Thick Client application | No |
| `GIT_DRS_GLOBUS_DESTINATION_COLLECTION` | UUID of a writable collection exposing this machine | No |
| `WORK_ROOT` | Local directory where the test creates disposable repositories | No |
| `GLOBUS_DESTINATION_ROOT_PATH` | The same directory as viewed inside the destination collection | No |

The remaining values in `.env.example` match the supplied
[`../../syfon/local.yaml`](../../syfon/local.yaml). Change them only if you
change the local Syfon configuration. No Globus token or client secret belongs
in `.env`.

## 1. Register a Globus client

If your organization already provides a git-drs client ID, use it and skip
client registration. Otherwise:

1. Open [Globus Developer Settings](https://app.globus.org/settings/developers).
2. Create or select a project, then register a **Thick Client** application.
3. Register this native-application redirect URL:
   `https://auth.globus.org/v2/web/auth-code`.
4. Copy the application's **Client ID** into
   `GIT_DRS_GLOBUS_CLIENT_ID` in `.env`.

A Thick Client is the correct type for an installed command-line program. It
uses PKCE and does not have a client secret. See the official
[Globus Auth application-registration guide](https://docs.globus.org/api/auth/developer-guide/#register-app).

## 2. Create or choose the destination collection

The destination is where Globus writes downloaded bytes. For a laptop or
workstation, the simplest choice is a Globus Connect Personal collection:

1. Install [Globus Connect Personal](https://docs.globus.org/globus-connect-personal/install/)
   for macOS, Windows, or Linux.
2. Start it, sign in, and complete its setup. Setup creates your personal
   collection; keep Globus Connect Personal running during the test.
3. Create `WORK_ROOT`, such as `/Users/alice/globus-tests`.
4. In Globus Connect Personal's **Access** or **Accessible Folders** settings,
   add `WORK_ROOT` with read/write access.
5. Permit access to hidden files under `WORK_ROOT`. git-drs transfers into
   `.git/lfs/objects`, so a setting that denies hidden files will break the
   download with `Path not allowed`.
6. Open [Globus File Manager](https://app.globus.org/file-manager), select your
   personal collection, browse to `WORK_ROOT`, and confirm that you can create
   or transfer a file there.
7. Copy the collection UUID shown by Globus into
   `GIT_DRS_GLOBUS_DESTINATION_COLLECTION`.

The official platform guides explain
[macOS accessible-directory settings](https://docs.globus.org/how-to/globus-connect-personal-mac/)
and [how to diagnose `Path not allowed`](https://docs.globus.org/globus-connect-personal/troubleshooting-guide/#path-not-allowed).
A personal collection can work from behind a firewall because Globus Connect
Personal establishes outbound connections; it must remain connected.

An institutional collection also works when its administrator grants write
access and tells you both its collection UUID and the collection path mapping
for `WORK_ROOT`.

## 3. Map the local path to the collection path

`WORK_ROOT` and `GLOBUS_DESTINATION_ROOT_PATH` identify the same directory from
two viewpoints:

```dotenv
WORK_ROOT=/Users/alice/globus-tests
GLOBUS_DESTINATION_ROOT_PATH=/globus-tests
```

To find the second value, browse to `WORK_ROOT` in
[Globus File Manager](https://app.globus.org/file-manager) and copy the value
from its **Path** field. Use the exact collection-absolute path, including a
leading `/`. Do not put a local filesystem path there unless that is exactly
how the collection exposes it.

Copy and edit the environment file:

```bash
cd tests/globus-user-integration-test
cp .env.example .env
```

## 4. Start local Syfon

From a second terminal at the repository root:

```bash
cd syfon
go run . serve --config local.yaml
```

Keep Syfon running at `http://localhost:8080`. The supplied configuration uses
scope `example/tutorial`, bucket `local-bucket`, and basic credentials
`drs-user` / `drs-pass`, matching `.env.example`.

## 5. Authorize git-drs with Globus

From the integration-test directory, load the client ID and log in:

```bash
set -a
source .env
set +a

git drs auth globus login \
  --scope https://auth.globus.org/scopes/6c54cade-bde5-45c1-bdea-f4bd71dba2cc/data_access
git drs auth globus status
```

Open the URL printed by `login`, sign in, approve the requested Transfer and
tutorial-collection consent, then paste the authorization code back into the
terminal. The tutorial collection UUID and files are documented in the
[official Globus transfer tutorial](https://docs.globus.org/guides/tutorials/manage-files/transfer-files/).

Do not invent a `data_access` scope for the destination. If Globus returns
`ConsentRequired`, rerun login using the exact `required_scopes` value reported
by Globus. An `unknown scopes` response means that the requested scope does not
exist for that collection. Globus explains collection-dependent consent in the
[Transfer API overview](https://docs.globus.org/api/transfer/overview/#data-access-consent).

git-drs stores the resulting refresh credential in the operating system's user
configuration directory and refreshes access tokens automatically. It does not
use or share credentials from the Globus CLI.

## 6. Run and inspect

```bash
scripts/tutorial.sh
```

The script:

1. creates a disposable repository and local bare Git remote beneath
   `WORK_ROOT`;
2. stages the public tutorial files into the destination collection;
3. creates and registers one DRS object per file;
4. prints the pointer files and DRS records before hydration;
5. downloads all three objects in one Globus batch; and
6. verifies their checksums and leaves the repository for inspection.

View submitted tasks in [Globus Activity](https://app.globus.org/activity).

## Common failures

- `ConsentRequired`: repeat `git drs auth globus login --scope ...` with the
  exact scope in the error.
- `Path not allowed`: expose `WORK_ROOT` and permit hidden `.git` paths in
  Globus Connect Personal.
- `Not authorized for that endpoint`: verify the destination collection UUID,
  identity, write permission, and that Globus Connect Personal is running.
- `Unable to connect ...:<port>`: review the
  [Globus Connect Personal troubleshooting guide](https://docs.globus.org/globus-connect-personal/troubleshooting-guide/)
  with local network/security administrators.
- A task stalls or fails: open [Globus Activity](https://app.globus.org/activity)
  and inspect its event log.
