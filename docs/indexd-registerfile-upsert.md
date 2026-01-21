# ADR 0001: Configure RegisterFile upsert/bucket checks via git LFS config

## Status
Accepted

## Context
The Indexd `RegisterFile` flow needs toggles for:
- whether to upsert indexd records (create when no matching project record exists, or replace by deleting and re-registering when a ma
- whether to check bucket existence before uploading (Unimplemented, currently always checks and skips upload if already present)

These toggles must be controlled per-repository using git LFS configuration (`git config` entries under `lfs.customtransfer.drs.*`). This keeps behavior in repo-local configuration and avoids coupling to remote YAML configuration.

## Decision
Read `lfs.customtransfer.drs.upsert` from git config during Indexd client initialization. Missing values default to `false`. Invalid values fail initialization with a clear error.

## Before

```mermaid
flowchart TD
    A[RegisterFile] --> B[Query indexd by hash]
    B --> C{Matching project record}
    C -- Yes --> D[Reuse existing record]
    C -- No --> E[Build DRS and indexd record]
    E --> F[POST indexd register]
    F --> G[Defer cleanup on failure]
    D --> H[Prepare records for download]
    G --> H
    H --> I[Call drs objects and access to get signed URL]
    I --> J[Check bucket via signed URL]
    J --> K{Downloadable}
    K -- Yes --> L[Skip upload]
    K -- No --> M[Upload to bucket]
```

## After


### fresh (no existing record)

```mermaid
flowchart TD
    A[RegisterFile] --> B[POST indexd register]
    B --> H[Prepare records for download]
    H --> J[Upload to bucket]
```

### force (retry on indexd register error)

```mermaid
flowchart TD
    A[RegisterFile] --> B[POST indexd register]
    B --> C{Register success}
    C -- Yes --> D[Prepare records for download]
    C -- No --> E[Enable force push]
    E --> F[Query indexd by hash]
    F --> G{Matching project record}
    G -- Yes --> H[Reuse existing record]
    G -- No --> I[POST indexd register]
    H --> D
    I --> D
    D --> J{force push}
    J -- No --> K[Upload to bucket]
    J -- Yes --> L[Get signed URL and check bucket]
    L --> M{Downloadable}
    M -- Yes --> N[Skip upload]
    M -- No --> K
```

## Consequences
- Operators can control behavior using `git config` without editing remote YAML.
- Defaults remain disabled when keys are missing.
- Misconfigured values fail fast during initialization.
