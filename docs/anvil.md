# Use AnVIL data references with git-drs

This guide is for AnVIL users who want to version a dataset layout in Git
without copying controlled-access data into Git. A Git commit contains stable
DRS references and public resolver settings only. Each person downloads data
with their own Google identity and AnVIL authorization.

## Before you begin

Install Git, `git-drs`, the Google Cloud CLI, and obtain access to both the Git
repository and the referenced AnVIL data. Establish Application Default
Credentials (ADC) for your user:

```bash
gcloud auth application-default login
```

ADC is local user state. Never copy the ADC JSON file into the repository.

## Configure an AnVIL repository

Create and commit `.git-drs/config.yaml`:

```yaml
version: 1
default_remote: anvil
remotes:
  anvil:
    type: terra
    endpoint: https://data.terra.bio
    auth: google-adc
    mode: read-only
```

The tracked schema intentionally permits only the public Terra endpoint,
Google ADC authentication selection, and read-only mode. Tokens, arbitrary
headers, credential paths, signed URLs, unknown fields, non-HTTPS endpoints,
and embedded URL credentials are rejected. Clone-local Git configuration can
override public settings when an organization uses another trusted resolver.

## Publish one reference

Use the configured remote and choose the path that the data should occupy:

```bash
git drs add-ref --remote anvil \
  drs://<authority>/<object-id> data/sample.cram
git add .git-drs/config.yaml .gitattributes data/sample.cram
git commit -m "Reference AnVIL sample"
git push
```

For a Terra remote, `add-ref` authenticates with your ADC, validates metadata,
and writes a small pointer. The pointer retains the canonical DRS URI even when
the record has a SHA256 checksum. It never contains an access token or signed
download URL. Destination paths must stay inside the Git repository.

AnVIL remotes are read-only: publish pointers with ordinary `git push`.
`git drs push` refuses a Terra remote because that command uploads payloads to
writable DRS providers.

## Clone and hydrate as another user

The consumer authenticates independently and then hydrates all pointers:

```bash
gcloud auth application-default login
git clone <git-repository>
cd <repository>
git drs pull
```

Hydrate only selected paths with an include pattern:

```bash
git drs pull -I "data/*.cram"
```

Cloning the Git repository does **not** grant AnVIL data access. A user without
permission can inspect reference paths and DRS URIs but receives an
authorization error when hydration resolves the object.

## Security and troubleshooting

* `google application default credentials are unavailable`: run
  `gcloud auth application-default login` as the current user and retry.
* `not authorized to access AnVIL DRS object`: confirm that the ADC identity has
  access to the controlled dataset. Do not ask another user to share ADC files.
* `AnVIL DRS object not found`: verify the committed URI and that the configured
  resolver supports its authority.
* Repository configuration errors are intentionally strict. Remove secret or
  unknown fields from `.git-drs/config.yaml`; place supported local overrides
  in Git configuration instead.
* Do not commit `.git/drs`, `.git/lfs`, Google credential files, bearer tokens,
  request headers, or resolved access URLs. Access URLs are temporary and are
  resolved again at download time.

For pointer details, see [Pointer files](pointer-files.md). For the prototype
design and acceptance criteria, see [AnVIL/Terra POC](anvil-terra-poc.md).
