# DRS compact identifiers, remote routing, and authentication discovery

## Summary

`git-drs` currently treats a DRS URI as an object identifier, not as a
replacement for remote configuration. Remote configuration continues to store
a concrete HTTP endpoint and provider-specific settings.

The authority in a `drs://` URI participates in resolution in some code paths,
but it is not universally treated as a directly reachable HTTPS hostname. This
distinction is important because a DRS authority can be an identifier namespace
rather than a DNS name.

The GA4GH DRS service-info operation can be used as one input to service and
authentication discovery. `git-drs` currently probes service-info for Terra
health checks, but it does not yet infer or persist an authentication method
from the response.

## Does a DRS compact identifier replace the configured remote?

No. There are two separate concepts:

- A DRS URI or compact identifier, such as `drs://drs.anv0:v2_...`, identifies
  a DRS object and its authority.
- A configured `git-drs` remote contains the concrete HTTP endpoint and any
  provider-specific configuration needed to contact the service.

The current configuration model stores an endpoint or base URL for Gen3,
Terra, and local remotes. These values are persisted as
`drs.remote.<name>.endpoint` in repository configuration.

The proposed unified remote interface may accept maintained aliases such as
`terra`, `cgc`, and `synapse`. These aliases would expand to concrete,
versioned endpoint presets; they are conveniences, not DRS object identifiers
or separate protocols.

The local compact OID described in the DRS identity ADR is another distinct
concept. The DRS URI is the canonical remote object identity, while the derived
OID is only a compact, filesystem-safe key for pointers and cache storage. The
derived OID does not identify or configure the DRS server.

## Is the `drs://` authority resolved as a hostname?

Partially, depending on the URI form and resolution path.

### Slash-form URI

For a URI such as:

```text
drs://source.example.org/object-1
```

the `add-ref` resolution path:

1. parses the URI authority;
2. looks for a configured remote whose endpoint host matches that authority;
3. uses that configured remote, including its authentication, when one matches;
4. otherwise derives `https://source.example.org` and attempts an anonymous
   DRS object lookup.

This fallback allows a complete slash-form DRS URI to direct resolution to its
source service without incorrectly sending it to the selected primary remote.

### Compact colon-form URI

For a compact identifier such as:

```text
drs://drs.anv0:v2_example
```

the AnVIL resolver understands that the colon separates the authority from the
object ID; it is not a network port separator. It extracts `drs.anv0` as the
authority and `v2_example` as the object ID.

The AnVIL resolver does not convert `drs.anv0` into an HTTPS endpoint. Instead,
it routes the identifier through the configured, trusted Terra/AnVIL resolver
endpoint. In this case the DRS authority is a namespace used by the resolver,
not necessarily a directly reachable DNS host.

The direct `add-ref` source-host parser currently expects an object ID in the
URI path. Compact colon-form identifiers therefore do not use the same
authority-derived anonymous fallback as slash-form identifiers; they depend on
the provider-specific resolver path.

### Recommended routing rule

The authority should participate in routing, but `git-drs` should prefer a
trusted authority-to-endpoint mapping or a configured resolver. It should not
universally assume that every DRS authority can safely be contacted as
`https://<authority>`.

## Can service-info determine how authentication works?

The GA4GH DRS service-info operation is available at the conventional route:

```http
GET <endpoint>/ga4gh/drs/v1/service-info
```

For Terra remotes, `git drs ping` already sends this request, treats a
successful response as a health check, and prints the service-info response.
The request currently uses the default HTTP client.

`git-drs` does not currently use that response to:

- select bearer, Basic, Google ADC, or provider-helper authentication;
- identify a credential source;
- persist a discovered authentication method;
- discover authentication for every remote type.

Service-info can be one trusted discovery input, but it may not contain enough
information to configure authentication completely. A response might identify
the service without specifying all of the OAuth issuer, audience, scopes,
credential source, or provider-specific login workflow required by a client.

The proposed `--auth auto` behavior therefore uses a deterministic precedence:

1. an explicit credential whose type implies one authentication method;
2. the maintained default from a trusted endpoint alias;
3. authentication metadata returned by trusted service discovery;
4. one installed provider helper for the detected provider;
5. anonymous access only after an anonymous capability probe succeeds.

If discovery is absent, conflicting, or advertises multiple usable methods,
remote setup should require an explicit authentication choice. It must not try
each locally available credential against the server. Once selected, the
authentication method should be persisted so normal commands have stable
network behavior; an explicit diagnostic command may rerun discovery.

## Current behavior at a glance

| Question | Current behavior |
| --- | --- |
| Does a DRS compact identifier replace the configured endpoint? | No. Remote endpoints remain in configuration. |
| Is the `drs://` authority used for routing? | Partially. Slash-form external references can derive an HTTPS endpoint; AnVIL compact identifiers use the configured trusted resolver. |
| Can `git-drs` call service-info? | Yes. `git drs ping` calls it for Terra remotes. |
| Does service-info automatically configure authentication? | No. Authentication discovery is proposed but not implemented. |
| Should every authority be treated as an HTTPS hostname? | No. Prefer a trusted mapping or resolver because an authority may be a namespace rather than a reachable host. |
