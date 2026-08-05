# Globus access methods

`git-drs` can hydrate DRS objects whose selected access URL uses the Globus
transport form:

```text
globus://<source-collection-id>/<source-path>
```

The DRS server provides the source collection and path. Your local `git-drs`
configuration provides the destination collection and destination path prefix,
and `git-drs` submits a Globus Transfer API task to move the bytes.

## Requirements

Before using Globus-backed access methods, you need:

1. A Globus Auth access token for the Globus Transfer API scope:

   ```text
   urn:globus:auth:scope:transfer.api.globus.org:all
   ```

   Some collections also require collection-specific `data_access` dependent
   scopes. If Globus returns a consent or required-scope error, request a token
   that includes the scopes named in the error response.

2. A destination Globus collection that can write to the path where your
   repository cache is visible. For a laptop or workstation, this is commonly a
   Globus Connect Personal collection.

3. A `git-drs` remote that can resolve the DRS object and return a Globus access
   method.

## Configure Globus for `git drs pull`

Export these environment variables before running `git drs pull`:

```bash
export GIT_DRS_GLOBUS_TRANSFER_TOKEN='<globus-transfer-api-access-token>'
export GIT_DRS_GLOBUS_DESTINATION_COLLECTION='<destination-collection-id>'
# Optional: collection-relative directory that should contain the git-drs cache path.
export GIT_DRS_GLOBUS_DESTINATION_PATH_PREFIX='/incoming/git-drs'
```

If a DRS object advertises multiple access methods and you want to force the
Globus method, set one of these:

```bash
export GIT_DRS_ACCESS_METHOD=globus
# or, for compatibility with older examples:
export GIT_DRS_TRANSFER_PROVIDER=globus
```

Verify the token before pulling:

```bash
git drs auth globus
```

Then hydrate files normally:

```bash
git drs pull
```

## How destination paths are built

`git-drs` downloads into its local cache path first, then checks out the hydrated
file from that cache. For Globus transfers, the destination path sent to Globus
is:

```text
<GIT_DRS_GLOBUS_DESTINATION_PATH_PREFIX>/<git-drs-cache-path>
```

If `GIT_DRS_GLOBUS_DESTINATION_PATH_PREFIX` is unset, the cache path itself is
used. The destination collection must expose the resulting path and must be able
to write to it.

## Troubleshooting

### `Globus Transfer API token is required`

Set `GIT_DRS_GLOBUS_TRANSFER_TOKEN` to a Globus Auth access token for the
Transfer API `all` scope, then rerun:

```bash
git drs auth globus
```

### `Globus Transfer API authentication failed`

The token is missing, expired, revoked, or does not have the right Transfer API
scope. Refresh the token and include:

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
3. `GIT_DRS_GLOBUS_DESTINATION_PATH_PREFIX` points to a directory that exists or
   can be created by Globus.
4. The token includes any source/destination collection `data_access` scopes
   required by Globus.
5. Retry with the same environment after correcting token or collection access.

### Pull selected HTTPS instead of Globus

If the DRS object has multiple access methods, force Globus selection:

```bash
GIT_DRS_ACCESS_METHOD=globus git drs pull
```
