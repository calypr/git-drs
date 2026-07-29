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
pre { background: #000; border-radius: 12px; color: #fff; padding: 18px 22px; }
pre code, pre code.hljs { background: #000 !important; color: #fff; font-size: 16px; line-height: 1.4; }
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
| Add references with ADC-authorized metadata validation | Commit canonical DRS URI, size, and checksum | Clone, configure the Terra remote, and authenticate independently | Hydrate from a fresh authorized URL and verify bytes |

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
<pre><code class="language-bash">
gcloud auth application-default login

git drs add-ref --remote anvil \
  drs://authority/object-1 data/sample.cram

&#35; Validate a batch before writing
git drs add-ref --remote anvil \
  --manifest references.tsv --dry-run

git add .gitattributes data/
git commit -m "Add AnVIL data references"
git push
</code></pre>
</div>
<div>
<h3>Published</h3>
<ul>
<li>Canonical DRS URI pointers</li>
<li>Paths and Git history</li>
<li><code>.gitattributes</code></li>
<li>Only portable pointer-management state</li>
</ul>
<h3>Never published</h3>
<ul>
<li>Payload bytes or cache content</li>
<li>Tokens, headers, or ADC files</li>
<li>Signed download URLs</li>
</ul>
</div>
</div>

---

## A clean clone plus explicit remote setup

<div class="columns">
<div>
<pre><code class="language-bash">
gcloud auth application-default login

git clone &lt;git-repository&gt;
cd &lt;repository&gt;

&#35; .git/config is not cloned; recreate public remote settings.
git drs remote add anvil terra --checkout hydrate

&#35; Hydrate every authorized reference
git drs pull

&#35; Or only one dataset slice
git drs pull -I "data/*.cram"
</code></pre>
<blockquote><p>No author cache, local Git config, token, or signed URL is transferred.</p></blockquote>
</div>
<div>
<table>
<thead><tr><th>Stage</th><th>Action</th></tr></thead>
<tbody>
<tr><td><strong>Git</strong></td><td>Clone pointer and <code>.gitattributes</code></td></tr>
<tr><td><strong>User B</strong></td><td>Configure remote; supply ADC; choose paths</td></tr>
<tr><td><strong>Resolver</strong></td><td>Fetch current metadata and fresh access</td></tr>
<tr><td><strong>Cache</strong></td><td>Download, verify, atomically promote</td></tr>
<tr><td><strong>Worktree</strong></td><td>Hydrate only after validation</td></tr>
</tbody>
</table>
</div>
</div>

---

<!-- _class: compact -->

## AnVIL manifest to pointer-only GitHub repo

<div class="columns">
<div>
<h3>1. Configure and add references</h3>
<p>Select the dataset in the <a href="https://explore.anvilproject.org/files?filter=%5B%7B%22categoryKey%22%3A%22files.file_format%22%2C%22value%22%3A%5B%22.tsv%22%2C%22.tsv.gz%22%5D%7D%2C%7B%22categoryKey%22%3A%22datasets.title%22%2C%22value%22%3A%5B%22ANVIL_1000G_PRIMED_data_model%22%5D%7D%5D">AnVIL Data Explorer</a>, then download its <a href="anvil-data-explorer.png">manifest</a>.</p>
<pre><code class="language-bash">
git init
git drs remote add anvil terra

&#35; Download the TSV from AnVIL Data Explorer first.
manifest=/tmp/anvil-manifest-38dc7537.tsv
scripts/anvil-add-ref-commands.sh "$manifest" \
  &gt; /tmp/add-anvil-refs.sh
cat /tmp/add-anvil-refs.sh
bash /tmp/add-anvil-refs.sh
</code></pre>
</div>
<div>
<h3>2. Commit, hydrate, and publish</h3>
<pre><code class="language-bash">
git add .gitattributes '*.tsv'
git commit -m "Add references to AnVIL data"

&#35; Materialize only TSVs locally.
git drs pull -I "*.tsv"
git status

git branch -M main
git remote add origin \
  https://github.com/bwalsh/\
ANVIL_1000G_PRIMED_data_model.git
git push -u origin main
</code></pre>
</div>
</div>

> **Result:** the worktree contains hydrated TSV data; GitHub contains only DRS pointers and `.gitattributes`. Every clone must configure its own Terra remote.

---

## Portable pointer + safe configuration

<div class="columns">
<div>
<h3>Tracked pointer</h3>
<pre><code class="language-text">
version https://calypr.github.io/spec/v1
oid drs://authority/object-1
size 987654321
sha256 8d969eef…
</code></pre>
<p>The DRS URI stays canonical. A checksum describes content; it does not identify the AnVIL record.</p>
</div>
<div>
<h3>Repository-local Git configuration</h3>
<pre><code class="language-bash">
git drs remote add anvil terra --checkout hydrate
</code></pre>
<p>Remote metadata lives only in clone-local <code>.git/config</code>; every clone must recreate it. Credentials remain in the ADC provider store.</p>
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

## Component status: implemented, partial, and missing

<div class="columns3">
<div class="card"><span class="tag">IMPLEMENTED</span><h3>Focused coverage</h3><ul><li>ADC-backed resolver contract</li><li>Canonical pointer and cache key</li><li>Atomic size/SHA256 validation</li><li>Terra ping and push refusal</li></ul></div>
<div class="card"><span class="tag">PARTIAL</span><h3>Workflow coverage</h3><ul><li>Manifest validation is sequential</li><li>Selective pull is covered in isolation</li><li>Cache reuse is per clone</li><li>Remote setup is clone-local</li></ul></div>
<div class="card"><span class="tag">MISSING</span><h3>POC acceptance</h3><ul><li>Independent User A/User B journey</li><li>Production AnVIL contract proof</li><li>Expired-URL retry and concurrency</li><li>Denied-user and leak audit</li></ul></div>
</div>

| Arrange | Act | Assert |
|---|---|---|
| Separate homes, config, ADC, caches | Commit → clone → authenticate → pull | Verified bytes; isolated credentials |

---

<!-- _class: small -->

## Most original blockers are now closed

<div class="columns">
<div>
<h3>Implemented</h3>
<ul>
<li>ADC-backed <code>AnVILResolver</code> handles metadata and access.</li>
<li>Terra <code>add-ref</code> and pull use provider-neutral resolution.</li>
<li>Remote config selects behavior; <code>--remote-type</code> is deprecated.</li>
<li>Terra pointers retain DRS URI, size, and optional SHA256.</li>
<li>Cache keys are separate from content checksums.</li>
</ul>
</div>
<div>
<h3>Still incomplete</h3>
<ul>
<li>Every fresh clone must manually recreate the Terra remote in <code>.git/config</code>.</li>
<li>The production endpoint, OAuth scope, and object/access contracts are not certified end to end.</li>
<li>Manifest resolution and downloads lack bounded concurrency and retry.</li>
<li>An expired download URL is not re-resolved after an HTTP failure.</li>
<li>Independent two-user, denied-user, and credential-leak acceptance remain unverified.</li>
</ul>
</div>
</div>

> **Bottom line:** an implemented vertical slice still needs production-contract validation, resilience, and end-to-end proof.

---

<!-- _class: small -->

## Turn the vertical slice into a proven POC

<div class="columns3">
<div class="card"><span class="tag">P0 · VERIFY</span><h3>Prove production fit</h3><ul><li>Confirm endpoint and OAuth scopes</li><li>Certify object/access contracts</li><li>Test slash and compact DRS IDs</li><li>Run clean two-user clone/pull</li><li>Audit logs and history</li></ul></div>
<div class="card"><span class="tag">P1 · HARDEN</span><h3>Make it resilient</h3><ul><li>Safe clone setup mechanism</li><li>Expired-URL re-resolution</li><li>Bounded retry and concurrency</li><li>Cancellation and diagnostics</li><li>Clean-environment CI</li></ul></div>
<div class="card"><span class="tag">P2 · GENERALIZE</span><h3>Keep DRS composable</h3><ul><li>Provider-neutral resolver contract</li><li>CGC and Synapse fixtures</li><li>Capability discovery</li><li>Keep publishing provider-specific</li><li>Defer mutation and copying</li></ul></div>
</div>

---

## Decisions needed to start the POC

1. Which authoritative AnVIL resolver contract, endpoint, and OAuth scopes will we certify?
2. What stable test objects cover checksummed, non-checksummed, large, and denied cases?
3. Is the DRS-URI pointer extension compatible with every parser that must read it?
4. Should consumers always run `remote add`, or may a tracked, non-secret bootstrap configure `.git/config`?
5. Who owns the independent-user acceptance environment and compatibility matrix?

### Questions?

Detailed design and acceptance criteria are in [`docs/anvil-terra-poc.md`](anvil-terra-poc.md).
