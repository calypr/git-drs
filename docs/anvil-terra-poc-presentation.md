---
marp: true
title: AnVIL + Terra x git-drs — reference POC
description: A reference proof of concept for version-controlled dataset layouts
paginate: true
theme: git-drs
---

<!-- _class: cover -->
<!-- _paginate: false -->

<style>
/* @theme git-drs */
@import 'default';
:root { --navy: #10253f; --blue: #1d70a2; --cyan: #54c6be; --gold: #f4b942; --paper: #f6f8fb; --muted: #607084; }
section { background: var(--paper); color: #152334; font-family: Inter, ui-sans-serif, system-ui, sans-serif; padding: 64px 78px; }
section::after { color: #8b98a8; font-size: 16px; font-weight: 700; }
h1, h2, h3 { color: var(--navy); }
h1 { font-size: 56px; letter-spacing: -.04em; }
h2 { font-size: 40px; letter-spacing: -.025em; }
h3 { color: var(--blue); font-size: 23px; }
p, li { font-size: 20px; line-height: 1.4; }
code { background: #e8edf3; }
pre { background: var(--navy); border-radius: 12px; color: #eaf6ff; padding: 18px 22px; }
pre code { background: transparent; font-size: 16px; line-height: 1.4; }
blockquote { background: #fff7e3; border-left: 7px solid var(--gold); color: #152334; padding: 10px 18px; }
blockquote p { font-size: 18px; }
table { width: 100%; }
th { background: var(--navy); color: white; }
td, th { font-size: 17px; }
.cover { background: linear-gradient(130deg, #0d223b 0%, #164d70 62%, #1f7d82 100%); color: white; }
.cover h1 { color: white; font-size: 64px; margin-top: 110px; max-width: 850px; }
.cover strong { color: #75ddd5; }
.cover p { color: #d8edf3; font-size: 26px; max-width: 850px; }
.small p, .small li { font-size: 17px; }
.compact p, .compact li { font-size: 16px; line-height: 1.3; }
.columns { display: grid; grid-template-columns: 1fr 1fr; gap: 42px; }
.columns3 { display: grid; grid-template-columns: repeat(3, 1fr); gap: 28px; }
.card { background: white; border: 1px solid #dce3eb; border-radius: 12px; padding: 18px 22px; }
.card p, .card li { font-size: 17px; }
.tag { background: #dff4f1; border-radius: 99px; color: #176b68; display: inline-block; font-size: 14px; font-weight: 800; padding: 5px 10px; }
</style>

# AnVIL + Terra meets **git-drs**

Version-controlled dataset layouts—without copying controlled-access payloads into Git.

Reference proof of concept · July 2026

---

## Git moves references. AnVIL moves bytes.

| User A | Git | User B | AnVIL |
|---|---|---|---|
| Add references with ADC-authorized metadata validation | Commit canonical DRS URI, size, and checksum | Clone and authenticate with an independent Google identity | Hydrate from a fresh authorized URL and verify bytes |

<div class="columns3">
<div class="card"><h3>0 payload bytes</h3><p>committed to Git</p></div>
<div class="card"><h3>2 identities</h3><p>authenticate independently</p></div>
<div class="card"><h3>1 reference</h3><p>stays canonical and portable</p></div>
</div>

> **Security boundary:** Git visibility reveals paths and DRS identifiers; it never grants authorization to the underlying data.

---

## One reference model, four perspectives

<div class="columns">
<div class="card"><h3>Data reference author</h3><p>Publish a reviewable, versioned dataset layout—one object or a manifest—without downloading payloads.</p></div>
<div class="card"><h3>Data consumer</h3><p>Clone pointers, authenticate independently, then hydrate everything or only paths matching a pattern.</p></div>
<div class="card"><h3>Unauthorized reader</h3><p>Inspect repository history but receive a clear denial when attempting controlled-data hydration.</p></div>
<div class="card"><h3>Repository maintainer</h3><p>Review deterministic pointers, diagnose provider failures, and run clean-clone acceptance tests.</p></div>
</div>

**Out of scope:** uploads, record mutation, provider copying, remote GC, and workspace-table sync.

---

## Add once. Publish with ordinary Git.

<div class="columns">
<div>

```bash
gcloud auth application-default login

git drs add-ref --remote anvil \
  drs://authority/object-1 data/sample.cram

# Validate a batch before writing
git drs add-ref --remote anvil \
  --manifest references.tsv --dry-run

git add .git-drs/ .gitattributes data/
git commit -m "Add AnVIL data references"
git push
```
</div>
<div>

### Published

- Canonical DRS URI pointers
- Paths and Git history
- `.gitattributes`
- Allowlisted, non-secret remote configuration

### Never published

- Payload bytes or cache content
- Tokens, headers, or ADC files
- Signed download URLs
</div>
</div>

---

## A clean clone is enough

<div class="columns">
<div>

```bash
gcloud auth application-default login

git clone <git-repository>
cd <repository>

# Hydrate every authorized reference
git drs pull

# Or only one dataset slice
git drs pull -I "data/*.cram"
```

> No author cache, local Git config, token, or signed URL is transferred.
</div>
<div>

| Stage | Action |
|---|---|
| **Git** | Clone pointer and safe public config |
| **User B** | Supply ADC; choose all or include pattern |
| **Resolver** | Fetch current metadata and fresh access |
| **Cache** | Download, verify, atomically promote |
| **Worktree** | Hydrate only after validation |
</div>
</div>

---

<!-- _class: compact -->

## AnVIL manifest to pointer-only GitHub repo

<div class="columns">
<div>

### 1. Configure and add references

```bash
git init
git drs remote add terra anvil \
  --drs-endpoint https://data.terra.bio \
  --auth google-adc --mode read-only

# Download the TSV from AnVIL Data Explorer first.
manifest=/tmp/anvil-manifest-38dc7537.tsv
scripts/anvil-add-ref-commands.sh "$manifest" \
  > /tmp/add-anvil-refs.sh
cat /tmp/add-anvil-refs.sh
bash /tmp/add-anvil-refs.sh
```
</div>
<div>

### 2. Commit, hydrate, and publish

```bash
git add .git-drs/ .gitattributes '*.tsv'
git commit -m "Add references to AnVIL data"

# Materialize only TSVs locally.
git drs pull -I "*.tsv"
git status

git branch -M main
git remote add origin \
  https://github.com/bwalsh/\
ANVIL_1000G_PRIMED_data_model.git
git push -u origin main
```
</div>
</div>

> **Result:** the worktree contains hydrated TSV data; GitHub contains only DRS pointers and safe public configuration.

---

## Portable pointer + safe configuration

<div class="columns">
<div>

### Tracked pointer

```text
version https://calypr.github.io/spec/v1
oid drs://authority/object-1
size 987654321
sha256 8d969eef…
```

The DRS URI stays canonical. A checksum describes content; it does not identify the AnVIL record.
</div>
<div>

### Repository-local Git configuration

```bash
git drs remote add anvil terra --checkout hydrate
```

Remote metadata has one authoritative representation in `.git/config`; credentials remain in the provider store.
</div>
</div>

---

## Provider-neutral resolution breaks Syfon coupling

```text
Commands                 Resolver interface                 Identities
add-ref · pull · ping  -> GetObject · GetAccess          -> remote · cache · content
                              |
                   +----------+----------+
                   |                     |
            AnVILResolver          SyfonResolver
          Google ADC · routing   Gen3 · local behavior
```

### Design rules

- Commands depend on one resolver contract.
- The configured remote selects the resolver.
- Authentication and provider routing stay behind the interface.
- There is no user-facing `--remote-type` switch.

---

## Three identities—not one overloaded OID

<div class="columns3">
<div class="card"><span class="tag">REMOTE</span><h3>Canonical DRS URI</h3><p>Normalized provider record identity. Committed to Git and used for object and access resolution.</p></div>
<div class="card"><span class="tag">LOCAL</span><h3>Cache OID</h3><p>Filesystem-safe SHA256 of a versioned prefix plus normalized DRS URI. Never replaces remote identity.</p></div>
<div class="card"><span class="tag">CONTENT</span><h3>Content SHA256</h3><p>Durable integrity metadata, when supplied. Used to verify bytes—not to locate the record.</p></div>
</div>

```text
cache_oid = sha256("git-drs-anvil-ref:v1\n" + normalized_drs_uri)
```

> **Download invariant:** fresh access → temporary file → verify size/checksum → atomic cache promotion → hydrate worktree.

---

<!-- _class: small -->

## Good component coverage; one decisive gap

<div class="columns3">
<div class="card"><span class="tag">IMPLEMENTED</span><h3>Focused coverage</h3><ul><li>Resolver contract and errors</li><li>Terra remote and safe config</li><li>Canonical pointer/cache key</li><li>Terra ping and push refusal</li></ul></div>
<div class="card"><span class="tag">PARTIAL</span><h3>Workflow coverage</h3><ul><li>Manifest validation and dry run</li><li>Pull size/SHA256 checks</li><li>Selective hydration</li><li>Cache reuse behavior</li></ul></div>
<div class="card"><span class="tag">MISSING</span><h3>POC acceptance</h3><ul><li>Independent User A/User B state</li><li>Real or contract-faithful AnVIL</li><li>Expired URL and retry journey</li><li>Authorization leak audit</li></ul></div>
</div>

| Arrange | Act | Assert |
|---|---|---|
| Separate homes, config, ADC, caches | Commit → clone → authenticate → pull | Verified bytes; isolated credentials |

---

<!-- _class: small -->

## Most original blockers are now closed

<div class="columns">
<div>

### Implemented

- ADC-backed `AnVILResolver` handles metadata and access.
- Terra `add-ref` and pull use provider-neutral resolution.
- Remote config selects behavior; `--remote-type` is deprecated.
- Terra pointers retain DRS URI, size, and optional SHA256.
- Cache keys are separate from content checksums.
</div>
<div>

### Still incomplete

- Compact AnVIL IDs still require the trusted Terra resolver.
- Service-info does not discover auth, provider, or capabilities.
- Remote setup retains provider-specific command shapes.
- Downloads lack bounded concurrency, retry, cancellation, and expired-URL re-resolution.
- Production and independent two-user acceptance remain unverified.
</div>
</div>

> **Bottom line:** an implemented vertical slice still needs production-contract validation, resilience, and end-to-end proof.

---

<!-- _class: small -->

## Turn the vertical slice into a proven POC

<div class="columns3">
<div class="card"><span class="tag">P0 · VERIFY</span><h3>Prove production fit</h3><ul><li>Confirm endpoint and OAuth scopes</li><li>Certify object/access contracts</li><li>Test slash and compact DRS IDs</li><li>Run clean two-user clone/pull</li><li>Audit logs and history</li></ul></div>
<div class="card"><span class="tag">P1 · HARDEN</span><h3>Make it resilient</h3><ul><li>Trusted authority routing</li><li>Persist auth discovery</li><li>Expired-URL re-resolution</li><li>Bounded retry and concurrency</li><li>Remote diagnostics and CI</li></ul></div>
<div class="card"><span class="tag">P2 · GENERALIZE</span><h3>Keep DRS composable</h3><ul><li>Unify `remote add`</li><li>Resolver/auth interfaces</li><li>CGC and Synapse fixtures</li><li>Keep publishing provider-specific</li><li>Defer mutation and copying</li></ul></div>
</div>

---

## Decisions needed to start the POC

1. Which authoritative AnVIL resolver contract, endpoint, and OAuth scopes will we certify?
2. What stable test objects cover checksummed, non-checksummed, large, and denied cases?
3. Is the pointer extension compatible with every parser that must read it?
4. What is the supported override policy for tracked versus local remote configuration?
5. Who owns the independent-user acceptance environment and compatibility matrix?

### Questions?

The maintained design source is [`docs/anvil-terra-poc.md`](anvil-terra-poc.md).
