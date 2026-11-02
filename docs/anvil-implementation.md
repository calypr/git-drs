# AnVIL/Terra implementation notes

This page describes the current implementation. It is intentionally narrower
than the AnVIL proof-of-concept and ADR documents: those pages include proposed
work, while this page describes the code paths that run today.

## What "read-only" means

A Terra remote is read-only from the perspective of the DRS data service.
`git-drs` can read object metadata, resolve access URLs, and download payloads,
but it cannot upload payload bytes or register/update DRS records there.

The Git repository is still writable. An AnVIL reference is a small text file,
so it is published like any other Git content:

```text
git drs add-ref -> git add -> git commit -> git push
```

The two push commands therefore have different roles:

| Command | Terra/AnVIL behavior |
|---|---|
| `git push` | Pushes commits, including pointer files, `.gitattributes`, and optional tracked configuration, to the Git remote. It does not contact Terra. |
| `git drs push [anvil]` | Refuses before upload/registration or Git push begins because a Terra remote is read-only. |

There is no implemented command that uploads an existing local payload into
AnVIL/Terra or creates an AnVIL DRS record. `add-ref` only points at an object
that already exists and is already visible through the configured resolver.

## Configuration and runtime selection

The established configuration model is the clone-local Git config written by:

```bash
git drs remote add terra anvil \
  --drs-endpoint https://data.terra.bio \
  --auth google-adc \
  --mode read-only
```

It stores `drs.remote.*` values in `.git/config`, which Git does not commit or
copy to another clone. The PR additionally introduces an optional
clone-portable `.git-drs/config.yaml`:

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

`internal/config.loadRepositoryConfig` parses this file with strict YAML field
checking. The committed schema accepts only:

- remote type `terra`;
- an HTTPS endpoint with no embedded credentials;
- auth selector `google-adc`; and
- mode `read-only`.

Tokens, headers, credential paths, and unknown fields are rejected. Existing
`drs.*` values in clone-local Git configuration are loaded afterward and take
precedence. `git drs remote add terra ...` writes only the established
clone-local Git configuration; it does not create the optional tracked YAML.

`internal/remoteruntime.New` turns the selected config into a Terra
`GitContext`. Unlike a Gen3/Syfon context, a Terra context has no Syfon client,
project, bucket, or storage prefix. Terra-aware commands branch on
`RemoteType == terra` and use `internal/resolver` instead.

## Authentication and resolver requests

`resolver.NewAnVIL` uses Google's Application Default Credentials chain with
the cloud-platform scope and creates a refreshing OAuth HTTP client. A user
normally initializes those credentials with:

```bash
gcloud auth application-default login
```

For both metadata lookup and hydration, the configured endpoint is treated as
a trusted resolver for every DRS authority. Given `drs://authority/object`, the
resolver extracts `object` and calls:

```text
GET <endpoint>/ga4gh/drs/v1/objects/<object>
GET <endpoint>/ga4gh/drs/v1/objects/<object>/access/<access-id>
```

The OAuth client is used for these resolver calls. The returned access URL and
headers are used only for the payload request; they are not written to the
pointer, YAML config, or local metadata.

The resolver accepts both path-style DRS URIs
(`drs://authority/object`) and compact AnVIL identifiers
(`drs://authority:v2_object`). It lowercases the authority while normalizing
the URI for resolver metadata.

## Creating a reference

`git drs add-ref --remote anvil <drs-uri> <path>` performs these steps:

1. Load the effective repository/Git configuration and require the selected
   remote to exist.
2. Reject absolute destinations and lexical paths that escape the repository.
3. Create the ADC-backed AnVIL resolver and fetch authoritative object
   metadata. This is a metadata request; payload bytes are not downloaded.
4. Write a git-drs DRS pointer at the requested path.
5. Add exact `filter=drs` and `drs.route=ro` attributes for the path.
6. Store the resolved DRS object in clone-local metadata under `.git/drs`.

A Terra reference always retains the DRS URI as its pointer OID, even when the
DRS object reports a content SHA256:

```text
version https://calypr.github.io/spec/v1
oid drs://authority/object
size 12345
sha256 <optional-content-sha256>
```

The optional `sha256` line is verification metadata. It is not substituted for
the durable DRS identity. The files under `.git/drs` are clone-local and must
not be committed.

`drs.route=ro` records the intended path role in `.gitattributes`. Current
upload prevention is enforced at the selected Terra remote in `cmd/push`; the
route attribute itself is not consulted by the push implementation.

### Manifest mode

`git drs add-ref --manifest references.tsv --remote anvil` is a batch wrapper
over metadata resolution and pointer creation. It requires `drs_uri` and
`path`; `size` and `sha256` are optional assertions. It:

- parses the complete TSV and rejects invalid URIs, unsafe or duplicate paths,
  and malformed assertions;
- resolves every row and compares optional size/SHA256 assertions with the
  authoritative object before writing any pointer;
- sorts successful entries by destination path; and
- writes a pointer and exact read-only tracking rules for each entry.

`--dry-run` performs parsing and remote metadata validation without writing
pointers or `.gitattributes`. The batch write phase is not transactional: an
I/O error after validation can leave earlier rows written. Unlike single-item
mode, manifest mode currently does not write clone-local DRS metadata sidecars.

The separate `scripts/anvil-add-ref-commands.sh` helper consumes the AnVIL
export columns `files.drs_uri` and `files.file_name` and prints shell-escaped
single-item `add-ref` commands. It does not execute them.

## Cache identity and hydration

The pointer OID is a DRS URI, but the on-disk cache layout remains compatible
with the existing SHA256-shaped LFS fanout. `internal/lfs.ObjectPath` derives a
deterministic cache key as:

```text
sha256("git-drs-anvil-ref:v1\n" + normalized-drs-uri)
```

This derived hash is only a local path key. It is not a checksum of the payload
and must never be advertised as one.

`git drs pull [anvil]` then:

1. inventories pointer files in the current checkout and applies any `-I`
   include patterns;
2. maps each DRS URI pointer to its deterministic local cache path;
3. skips a complete cached object (size plus optional content SHA256);
4. fetches fresh metadata and the first advertised access method through the
   ADC-backed resolver;
5. streams the payload to a temporary file without persisting the signed URL;
6. validates size and, when present in the pointer, content SHA256;
7. atomically promotes the cache file and checks it out into the worktree; and
8. refreshes the index so Git's clean filter sees the hydrated path correctly.

The default filter configuration skips network hydration during checkout, so
clones initially retain pointers. Before using `git drs pull`, a fresh clone
must either run `git drs remote add terra ...` to create its local remote and
filters, or run `git drs init` when the optional tracked YAML already supplies
the remote. The clean filter preserves the indexed DRS pointer when the
hydrated bytes still match its checksum, or match the payload in the DRS-URI
cache when the service supplied no checksum. A modified payload is cleaned into
a new content-SHA256 pointer instead of being mistaken for the original AnVIL
reference.

## Code map

| Area | Responsibility |
|---|---|
| `internal/config/config.go`, `internal/config/remote.go` | Strict committed config, local overrides, and the Terra remote model |
| `internal/remoteruntime/runtime.go` | Selects a Terra runtime without constructing a Syfon client |
| `internal/resolver/resolver.go` | ADC, DRS URI normalization, metadata/access lookup, and ephemeral download |
| `cmd/addref/add-ref.go` | Single and TSV reference creation |
| `internal/lfs/inventory.go` | DRS pointer parsing/writing and optional SHA256 metadata |
| `internal/lfs/object_path.go` | Deterministic DRS-URI-to-cache-key mapping |
| `cmd/pull/main.go` | Terra-aware download selection, verification, and hydration |
| `internal/filter/clean.go` | Keeps an unchanged hydrated DRS reference clean in Git |
| `cmd/push/main.go` | Rejects managed data pushes to Terra remotes |
| `internal/gitrepo/gitattributes_tracking.go` | Adds exact filter and `drs.route=ro` tracking rules |

## Current boundaries

- Terra support is reference-and-download only; upload, registration, update,
  and deletion are not implemented.
- Access-method selection currently uses the first advertised method and
  requires it to have an access ID.
- Automatic network smudge is not a Terra-aware path; use `git drs pull`.
- Pointer creation does not grant access. Every clone resolves and downloads
  using that user's ADC identity and AnVIL permissions.
- The configured endpoint must be able to resolve the authorities committed in
  the repository; the DRS URI authority is not contacted directly on the Terra
  path.
