# Globus access methods

`git-drs` can hydrate DRS objects whose selected access URL uses the Globus
transport form:

```text
globus://<source-collection-id>/<source-path>
```

The DRS server provides the source collection and path. Your local `git-drs`
configuration provides a destination collection whose root exposes the Git
repository, and `git-drs` submits a Globus Transfer API task that writes directly
to the repository's LFS cache path.

## Requirements

Before using Globus-backed access methods, you need:

1. A registered Globus native application client ID. Configure its redirect URL
   as `https://auth.globus.org/v2/web/auth-code`. `git-drs` requests the Globus
   Transfer API scope:

   ```text
   urn:globus:auth:scope:transfer.api.globus.org:all
   ```

   Some collections also require collection-specific `data_access` dependent
   scopes. If Globus returns a consent or required-scope error, repeat login
   with each scope named in the error response:

   ```bash
   git drs auth globus login --scope '<required-scope>'
   ```

2. A destination Globus collection that can write to the path where your
   repository cache is visible. For a laptop or workstation, this is commonly a
   Globus Connect Personal collection. This destination is normally specific to
   each user or execution environment; it is not supplied by the DRS object or
   shared through the Git repository.

3. A `git-drs` remote that can resolve the DRS object and return a Globus access
   method.

## Configure Globus for `git drs pull`

Log in once and configure the destination collection:

```bash
export GIT_DRS_GLOBUS_CLIENT_ID='<native-application-client-id>'
git drs auth globus login
export GIT_DRS_GLOBUS_DESTINATION_COLLECTION='<destination-collection-id>'
```

The login command prints an authorization URL. Open it, approve access, and
paste the returned authorization code. Credentials are stored with owner-only
permissions in the user configuration directory and refreshed automatically.
Use `git drs auth globus status` to verify them and
`git drs auth globus logout` to remove them.

`git-drs` calls Globus Auth and Transfer directly through `globus-go-sdk`; the
separate Globus CLI does not need to be installed.

Automation may instead provide a non-persistent access-token override:

```bash
export GIT_DRS_GLOBUS_TRANSFER_TOKEN='<globus-transfer-api-access-token>'
```

## Credential sources and storage

Credential selection is deterministic:

1. `GIT_DRS_GLOBUS_TRANSFER_TOKEN`, when non-empty, is used as a static token.
2. Otherwise, `git-drs` loads the credential saved by
   `git drs auth globus login`.

The static environment token is not stored or refreshed. Stored login
credentials include a refresh token; the SDK refreshes the access token before
expiry and saves the replacement token for later commands.

By default, credentials are stored under the operating system's user
configuration directory in `git-drs/globus-tokens.json`. The directory is mode
`0700` and the credential file is mode `0600`. Set
`GIT_DRS_GLOBUS_TOKEN_FILE` to use a different local path. Never place that
file inside a repository or commit it.

| Variable | Purpose |
| --- | --- |
| `GIT_DRS_GLOBUS_CLIENT_ID` | Native application client ID used during first login and saved locally for refresh. |
| `GIT_DRS_GLOBUS_CLIENT_SECRET` | Optional confidential-client secret; native applications leave it unset. |
| `GIT_DRS_GLOBUS_TRANSFER_TOKEN` | Static, non-persistent override for automation; no automatic refresh. |
| `GIT_DRS_GLOBUS_TOKEN_FILE` | Optional path override for SDK token storage. |
| `GIT_DRS_GLOBUS_DESTINATION_COLLECTION` | Destination collection used for transfers; not an authentication secret. |

For a confidential client, `GIT_DRS_GLOBUS_CLIENT_SECRET` must remain available
when a refresh occurs. Prefer a native/public client for interactive desktop or
command-line use so no client secret is required.

The `globus://<source-collection-id>/<source-path>` URL identifies the source
collection only. `GIT_DRS_GLOBUS_DESTINATION_COLLECTION` identifies the
collection that exposes this user's local repository. Each user, workstation,
runner, or compute environment must configure an accessible destination
collection. Several users may intentionally share a managed institutional or
project collection, but that remains environment configuration rather than
tracked repository metadata.

If a DRS object advertises multiple access methods and you want to prefer the
Globus method, set one of these:

```bash
export GIT_DRS_ACCESS_METHOD=globus
# or, for compatibility with older examples:
export GIT_DRS_TRANSFER_PROVIDER=globus
```

Verify authentication before pulling:

```bash
git drs auth globus status
```

Then hydrate files normally:

```bash
git drs pull
```

From a Git user's perspective, a Globus-backed file behaves like any other
tracked DRS file. Its pointer stays at the repository path chosen by the user,
and pull or smudge hydrates that path through the normal LFS cache. There is no
per-file Globus destination, path prefix, or separate checkout workflow.

The normal file lifecycle is unchanged:

```bash
git drs track "data/*.bam"
git add .gitattributes data/sample.bam
git commit -m "Track sample data"
git drs push
git drs pull
```

Globus is an access method, not a distinct file type or push mode. `track`,
`ls-files`, `push`, include filters, pointer files, and hydrated worktree paths
behave the same regardless of the access method later selected by `pull`.

## How destination paths are built

`git-drs` downloads into its local cache path first, then checks out the hydrated
file from that cache. For Globus transfers, the destination path sent to Globus
is the collection-absolute LFS cache path:

```text
/.git/lfs/objects/<oid path>
```

The destination collection root must expose the repository root so this writes
to the same LFS cache consumed by pull and smudge. The collection ID and its
root mapping are transport configuration; they do not change the file's Git
path or cache path.

## Encoding a single-file Globus transfer in DRS

The GA4GH DRS [`AccessMethod`](https://github.com/ga4gh/data-repository-service-schemas/blob/master/openapi/components/schemas/AccessMethod.yaml)
schema can cleanly encode the source half of a Globus transfer, but not a
complete source-to-destination transfer. This separation is intentional: DRS
describes how to access an object, while the consuming client chooses where to
place it.

A direct Globus source can be represented as:

```yaml
type: globus
access_url:
  url: globus://6c54cade-bde5-45c1-bdea-f4bd71dba2cc/data/sample.bam
available: true
```

The fields have these meanings:

- `type` selects the Globus transfer handler.
- The URL authority is the source Globus collection UUID.
- The URL path is the source path relative to that collection's root.
- `available` indicates whether the source is immediately accessible.

For a protected or dynamically resolved source, use an `access_id`:

```yaml
type: globus
access_id: globus-primary
authorizations:
  supported_types:
    - BearerAuth
available: true
```

The client passes `globus-primary` to the object's DRS `/access` endpoint. A
successful response supplies the source locator:

```yaml
url: globus://6c54cade-bde5-45c1-bdea-f4bd71dba2cc/data/sample.bam
```

This permits the DRS service to authorize access before disclosing the source
collection or path. The `authorizations` field describes authorization for the
DRS `/access` request; it does not authorize calls to the Globus Transfer API.

### Parameter ownership

| Transfer parameter | Appropriate owner |
| --- | --- |
| Source collection | DRS `access_url` |
| Source path | DRS `access_url` |
| Object size and checksum | Parent DRS object |
| Destination collection | Client configuration |
| Destination path | Client-selected cache path |
| Globus token and dependent scopes | Client credential store |
| Submission ID | Obtained dynamically from Globus |
| Label, deadline, and sync level | Client transfer policy |
| Task ID and status | Returned by Globus |

The corresponding single-file
[Globus transfer request](https://docs.globus.org/api/transfer/task_submit/)
resembles:

```json
{
  "DATA_TYPE": "transfer",
  "submission_id": "<obtained-from-globus>",
  "source_endpoint": "6c54cade-bde5-45c1-bdea-f4bd71dba2cc",
  "destination_endpoint": "<client-destination-collection>",
  "sync_level": 3,
  "DATA": [
    {
      "DATA_TYPE": "transfer_item",
      "source_path": "/data/sample.bam",
      "destination_path": "/.git/lfs/objects/...",
      "recursive": false
    }
  ]
}
```

Do not encode the destination, Globus token, submission ID, or task options in
`access_id`, `region`, URL query parameters, or `headers`:

- `access_id` is an opaque identifier used to resolve access.
- `region` is cloud-region metadata, not a collection identifier.
- `headers` apply when fetching the returned URL; they are not a general
  credential envelope.
- The destination belongs to the caller and may differ on every invocation.
- Tokens, submission IDs, and task IDs are ephemeral or sensitive.

The interoperable responsibility split is therefore:

```text
DRS AccessMethod = source locator
DRS object       = object identity, size, checksum
Client config    = destination and credentials
Client policy    = transfer options
Globus API       = submission ID, execution, and task status
```

Encoding an entire third-party transfer in `AccessMethod` would require a new
structured transfer schema. Packing those parameters into the existing fields
would create a private convention rather than interoperable DRS.

## Troubleshooting

### `Globus authentication is required`

Run the interactive login:

```bash
export GIT_DRS_GLOBUS_CLIENT_ID='<native-application-client-id>'
git drs auth globus login
```

For automation, set `GIT_DRS_GLOBUS_TRANSFER_TOKEN` to a pre-issued Globus Auth
access token with the Transfer API `all` scope.

### `Globus Transfer API authentication failed`

The token is revoked or does not have the right Transfer API scope. Log in
again and include:

```text
urn:globus:auth:scope:transfer.api.globus.org:all
```

If the error body mentions dependent scopes or consent, also include the listed
collection `data_access` scopes.

### `Globus destination collection is required`

Set the destination collection ID:

```bash
export GIT_DRS_GLOBUS_DESTINATION_COLLECTION='<destination-collection-id>'
```

### Transfer task fails or never completes

Check these items:

1. The source collection ID and path in the DRS `globus://` URL are correct.
2. The destination collection is activated and writable.
3. The destination collection root exposes the repository root.
4. The token includes any source/destination collection `data_access` scopes
   required by Globus.
5. Retry with the same environment after correcting token or collection access.

### Pull selected HTTPS instead of Globus

If the DRS object has multiple access methods, prefer Globus selection:

```bash
GIT_DRS_ACCESS_METHOD=globus git drs pull
```
