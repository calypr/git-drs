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

- `auto`: prefer HTTPS, then Globus among implemented data-plane handlers.
  S3 or GS access IDs remain usable when DRS resolves them to HTTPS. Server
  array order is not preference.
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
4. committed `.git-drs/drs-policies.yaml` policy;
5. the built-in `auto` policy.

Configure a remote without storing credentials in Git:

```bash
git config --local drs.remote.research.access-method prefer:globus
```

Repositories may commit a canonical endpoint and shared default:

```yaml
version: 1
remotes:
  research:
    endpoint: https://drs.example.org
    provider: gen3
    auth: bearer
    scope: organization/project
    selection:
      access_method: prefer:globus
    transfer:
      globus:
        allowed_source_collections:
          - 11111111-1111-1111-1111-111111111111
```

The source list is a repository constraint, not a trust anchor: missing means
no additional restriction, while an empty list disables every Globus source.
It cannot contain destination routing or credentials. Unknown fields and
unsupported policy versions fail before transfer planning.

A local endpoint and selection value override the committed defaults. Before
credentials can be sent to an authenticated committed endpoint, review and
trust the exact endpoint locally:

```bash
git config --local --add drs.trusted-endpoint https://drs.example.org
```

This confirmation is unnecessary when the endpoint itself is configured
locally. Globus destinations, routes, paths, and all credentials remain local.

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

HTTPS URLs are ready. DRS access IDs are resolved during planning and the
returned URL is then evaluated. Globus readiness requires a routed destination
collection plus either
`GIT_DRS_GLOBUS_TRANSFER_TOKEN` or credentials stored by
`git drs auth globus login`. Readiness is local; a credential can still be
rejected when it is used.

### Fallback boundary

DRS `/access` resolution is part of planning because it may reveal the Globus
source collection needed for destination routing. A preferred candidate may
fall back when `/access` resolution, URL validation, or routing fails. Once the
first byte request or Globus task submission begins, git-drs does not silently
switch providers.

### Aggregate diagnostics

When planning several objects, git-drs collects all selection failures before
returning them. Each diagnostic includes the object ID and each considered
method's readiness state and reason. For example:

```text
object one: no usable access method for require: globus=disabled (destination_collection_unmapped)
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
