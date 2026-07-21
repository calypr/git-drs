# Remote CLI and multi-DRS server design

## Status

Proposed. This document describes a migration target rather than the current
command-line contract.

## Problem

`remote add` currently exposes the implementation as three different command
shapes:

```text
git drs remote add gen3 [remote-name] <organization/project>
git drs remote add local <remote-name> <url> <organization/project>
git drs remote add terra <remote-name> --drs-endpoint URL --auth google-adc --mode read-only
```

This creates several avoidable distinctions:

- the provider is sometimes a command and sometimes effectively a deployment
  choice;
- the endpoint is inferred, positional, or a flag depending on the provider;
- `--cred`, `--token`, `--username`, and `--password` encode credential formats
  rather than a credential source;
- `--mode read-only` and `--auth google-adc` ask users to state the only values
  currently accepted;
- `--bucket` mixes DRS resolution with Gen3 storage and publishing policy;
- `--no-skip-smudge` is repository checkout policy, not a remote property; and
- `local` describes where a server is deployed, not which DRS protocol it
  implements.

Adding Cancer Genomics Cloud (CGC), Synapse, or another GA4GH DRS service by
copying this pattern would produce another provider subcommand and another set
of slightly different flags for every service.

## Proposed vocabulary

Use one command with two required concepts: a local remote name and a server
endpoint (or a well-known endpoint alias).

```text
git drs remote add <name> <endpoint-or-alias> [flags]

Flags:
  --scope <scope>             Optional provider-specific collection/project scope
  --auth <method>             auto, none, bearer, basic, google-adc, or provider-helper
  --credential <source>      Credential source, never an inline secret
  --provider <provider>      auto, ga4gh, gen3, terra, cgc, or synapse
  --storage <bucket/prefix>  Advanced write/registration storage override
  --checkout <mode>          pointers or hydrate
```

For example:

```sh
# Provider and capabilities discovered from service-info where possible.
git drs remote add cgc https://cgc-ga4gh-api.sbgenomics.com \
  --auth bearer --credential env:CGC_TOKEN

git drs remote add synapse https://repo-prod.prod.sagebase.org \
  --auth bearer --credential helper:synapse

# A preset may supply a public endpoint and authentication default.
git drs remote add anvil terra --scope my-billing-project/my-workspace

# Gen3 keeps its scope, but no longer needs a different grammar.
git drs remote add production https://example-gen3.org \
  --provider gen3 --scope PROGRAM/PROJECT \
  --credential file:~/.gen3/credentials.json
```

`<endpoint-or-alias>` should be an HTTPS URL by default. Aliases such as
`terra`, `cgc`, and `synapse` are conveniences that expand to versioned,
maintained presets; they are not separate protocols. `--provider` is an escape
hatch when discovery is absent or ambiguous, and should normally remain
`auto`.

### Defaults remove flags

- Discover the provider and supported features using GA4GH service-info and a
  small, maintained fingerprint table. Print the detected provider before
  persisting configuration.
- Infer `--auth auto` from the preset, a 401 challenge, and installed credential
  helpers. Do not silently downgrade from authenticated to anonymous access.
- Infer read-only versus writable from capability probing. Replace `--mode`
  with reported capabilities rather than a user assertion.
- Default checkout behavior from the repository's existing configuration.
  `--checkout` is an optional convenience on `remote add`; the canonical
  setting belongs to a repository-level `config` command.
- Prompt for a storage choice only when a write operation actually needs one.
  Keep `--storage` for automation, but do not make bucket selection part of
  adding a read-only DRS resolver.

### Credential sources

One `--credential` option should name where credentials come from:

```text
env:NAME        read a token from an environment variable
file:PATH       import/read a provider credential file
helper:NAME     invoke an OS, Git, or provider credential helper
stdin           read a secret without placing it in shell history
profile:NAME    use an existing provider profile
```

Inline `--token` and `--password` values should be deprecated because they are
visible in process listings and shell history. Basic authentication can still
be supported by a helper that returns a username and password. Public tracked
configuration contains only endpoint, provider, scope, and safe capability
hints; credential source selection and secrets remain clone-local.

## Separate the DRS protocol from provider extensions

Model a remote as a composition instead of one provider-specific client:

1. **Resolver** — the GA4GH DRS operations for object metadata and access
   methods/URLs.
2. **Authenticator** — anonymous, bearer, basic, Google ADC, or a provider
   helper that can refresh credentials.
3. **Namespace codec** — parsing and formatting the server's accepted DRS URI
   authority and object identifiers.
4. **Publisher** — optional upload, registration, checksum search, bucket
   mapping, and delete operations. These are not implied by DRS resolution.
5. **Capabilities** — discovered facts such as resolve, download, search,
   register, upload, and delete, cached with the remote configuration and
   rechecked by `git drs remote diagnose <name>`.

The generic resolver should implement the standard DRS object and access
flows. Provider adapters should be small and limited to real differences:
authentication, endpoint/URI normalization, pagination or response quirks,
and non-standard publishing APIs. Core code should select behavior by a
capability interface, not by tests such as `remote.type == "terra"`.

This separation also makes errors useful. A CGC or Synapse remote can be a
perfectly valid read-only resolver even when it cannot support the current
Gen3 checksum-search and registration workflow. `push` should fail early with
"remote does not advertise register/upload" rather than treating resolution
success as evidence that publishing is available.

## CGC and Synapse

Both services should first be treated as GA4GH DRS resolvers, then augmented
only where their documented behavior requires it.

### Cancer Genomics Cloud

The CGC adapter should provide a preset for its DRS base URL, bearer-token
credential help, and its accepted DRS URI authority/object identifier form.
It should use the generic object and access flow. Any CGC project browsing,
file upload, or file registration API belongs in an optional publisher adapter,
because those operations are outside the baseline DRS resolver contract.

The implementation must test whether an access response contains a directly
usable URL or requires the follow-up access endpoint, preserve documented
headers, and never persist a signed access URL. A project identifier, if needed
for non-DRS publishing, is `--scope`; it must not be required merely to resolve
a complete DRS URI.

### Synapse

The Synapse adapter should likewise provide its production endpoint/authority
preset and obtain a Synapse bearer credential through a helper. Resolution
should remain object-ID based and should not assume that a returned checksum is
itself a resolvable object ID.

Synapse project/entity discovery and mutation are separate Synapse REST
capabilities. If later supported for publishing, they should be implemented by
a Synapse publisher and exposed through capability checks, not folded into the
generic DRS resolver or made mandatory during `remote add`.

### Conformance fixtures

Add provider contract tests built from sanitized documented responses for:

- service-info discovery or the explicit fallback when it is unavailable;
- object metadata with one and multiple access methods;
- the access endpoint and expiring URL handling;
- bearer authentication and redaction of tokens and response bodies;
- 401/403, 404, 429, and server-error classification;
- DRS URI authority and percent-encoding normalization; and
- missing SHA-256 checksums (the canonical DRS URI remains the retrieval
  identity).

Live acceptance tests should be opt-in and credential-gated. Generic GA4GH
contract tests should run against every adapter so provider support cannot
silently diverge.

## Compatibility and migration

Introduce the unified form without immediately removing scripts:

1. Add `remote add <name> <endpoint-or-alias>` and store the compositional
   remote configuration.
2. Keep `remote add gen3`, `local`, and `terra` as hidden or deprecated
   compatibility shims that translate their arguments to the new model.
3. Add `git drs remote migrate` and make `remote list --verbose` display the
   translated endpoint, provider, auth source (never its value), and
   capabilities.
4. Warn on inline `--token`/`--password`, `--mode`, and `--no-skip-smudge` with
   the exact replacement command for at least one release cycle.
5. Remove legacy forms only in a major release or retain them indefinitely as
   thin aliases if maintenance cost is negligible.

The minimal common case then becomes memorable:

```sh
git drs remote add <name> <server>
```

Scope, authentication, publishing storage, and checkout policy appear only
when the server or workflow actually needs them.

## References

- [Cancer Genomics Cloud DRS API overview](https://docs.cancergenomicscloud.org/reference/drs-api-overview)
- [Synapse DRS controller REST documentation](https://rest-docs.synapse.org/rest/index.html#org.sagebionetworks.drs.controller.DrsController)
