# ADR A: Client Access-Method Selection Policy

- **Status:** Accepted
- **Date:** 2026-08-11
- **Scope:** Client selection policy only

## Context

A DRS object may advertise several `access_methods`, but DRS does not define
their preference order. A client must choose predictably, avoid methods it
cannot currently use, and explain why no choice was possible. This ADR does not
define new DRS metadata, credential exchange, or bulk transfer grouping.

## Decision

git-drs supports three policies:

- `auto`: choose the first ready method in the built-in order `https`, `s3`,
  `gs`, `globus`, then other method names lexically. Server array order is not
  preference.
- `prefer:<type>`: try the named type first, then fall back using the built-in
  order.
- `require:<type>`: use only the named type and fail during planning if it is
  not advertised or ready.

A bare method name in environment or Git configuration remains an alias for
`prefer:<type>` for compatibility.

### Preference precedence

The first configured source wins:

1. `git drs pull --access-method <type>` (strict; equivalent to
   `require:<type>`);
2. `GIT_DRS_ACCESS_METHOD`, then the legacy
   `GIT_DRS_TRANSFER_PROVIDER` environment variable;
3. repository-local remote configuration;
4. the built-in `auto` policy.

Configure a remote without storing credentials in Git:

```bash
git config --local drs.remote.research.access-method prefer:globus
```

Examples:

```bash
GIT_DRS_ACCESS_METHOD=require:globus git drs pull research
GIT_DRS_ACCESS_METHOD=prefer:globus git drs pull research
git drs pull research --access-method https
```

### Readiness states

Each candidate receives one local planning state:

- `ready`: the handler and required local configuration are present;
- `disabled`: the method is unavailable or its handler/credentials were not
  configured;
- `broken`: configuration exists but is invalid, expired without refresh, or
  the method has no usable URL or access ID.

HTTPS URLs and resolvable DRS access IDs are locally ready. Globus readiness
requires a destination collection plus either
`GIT_DRS_GLOBUS_TRANSFER_TOKEN` or credentials stored by
`git drs auth globus login`. Readiness is local; a credential can still be
rejected when it is used.

### Fallback boundary

Fallback occurs only while selecting a ready method. Once git-drs starts a DRS
`/access` request or a byte transfer, failure is returned for that selected
method. It does not silently switch providers after execution begins or after a
partial transfer.

### Aggregate diagnostics

When planning several objects, git-drs collects all selection failures before
returning them. Each diagnostic includes the object ID and each considered
method's readiness state and reason. For example:

```text
object one: no usable access method for require: globus=disabled (GIT_DRS_GLOBUS_DESTINATION_COLLECTION is not configured)
object two: no usable access method for require: globus=broken (stored Globus access token is expired and cannot be refreshed)
```

No credential values are included in diagnostics.

## Consequences

Selection is reproducible, explicit requirements never fall back, preferences
remain convenient, and SDK-stored Globus credentials participate in planning.
Remote configuration contains policy only; secrets stay in credential storage
or environment variables.

Service-info recommendations, new DRS metadata, generic capability plugins,
and bulk partition/common-method planning belong to separate ADRs.
