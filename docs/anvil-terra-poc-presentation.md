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

## DRS: object metadata + live access

<div class="columns">
<div>
<svg viewBox="0 0 900 300" width="100%" height="300" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <marker id="arrow" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
      <path d="M0,0 L0,6 L9,3 z" fill="#1d70a2"/>
    </marker>
    <linearGradient id="g1" x1="0" x2="1">
      <stop offset="0%" stop-color="#eaf3fb"/>
      <stop offset="100%" stop-color="#dff3f1"/>
    </linearGradient>
  </defs>
  <rect x="42" y="62" width="180" height="152" rx="20" fill="url(#g1)" stroke="#1d70a2" stroke-width="2"/>
  <text x="132" y="102" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Git repo</text>
  <text x="132" y="138" text-anchor="middle" font-size="18" fill="#10253f">pointer metadata</text>
  <text x="132" y="166" text-anchor="middle" font-size="18" fill="#10253f">DRS URI</text>
  <text x="132" y="194" text-anchor="middle" font-size="18" fill="#10253f">checksum + size</text>

  <rect x="322" y="36" width="260" height="204" rx="22" fill="#f6f8fb" stroke="#54c6be" stroke-width="2"/>
  <text x="452" y="78" text-anchor="middle" font-size="24" fill="#10253f" font-weight="700">DRS registry</text>
  <text x="452" y="116" text-anchor="middle" font-size="18" fill="#10253f">id / self_uri / name</text>
  <text x="452" y="146" text-anchor="middle" font-size="18" fill="#10253f">checksums / size</text>
  <text x="452" y="176" text-anchor="middle" font-size="18" fill="#10253f">description / version</text>
  <text x="452" y="206" text-anchor="middle" font-size="18" fill="#10253f">created_time / controlled_access</text>

  <rect x="670" y="62" width="180" height="152" rx="20" fill="#eaf5f3" stroke="#54c6be" stroke-width="2"/>
  <text x="760" y="102" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Access layer</text>
  <text x="760" y="138" text-anchor="middle" font-size="18" fill="#10253f">access_id</text>
  <text x="760" y="166" text-anchor="middle" font-size="18" fill="#10253f">HTTPS / GS / S3</text>
  <text x="760" y="194" text-anchor="middle" font-size="18" fill="#10253f">Globus / transfer APIs</text>

  <line x1="222" y1="138" x2="322" y2="138" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow)"/>
  <line x1="582" y1="138" x2="670" y2="138" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow)"/>
  <path d="M 452 240 C 540 260, 650 260, 760 230" fill="none" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow)" stroke-dasharray="8 8"/>
  <text x="610" y="260" text-anchor="middle" font-size="18" fill="#1d70a2">resolve live access + fetch bytes</text>
</svg>
</div>
<div>
<h3>Key idea</h3>
<ul>
<li>Git stores a stable reference to a DRS object, not the bytes.</li>
<li>DRS records describe the object identity, metadata, and the live access methods.</li>
<li>Clients resolve a fresh access URL at read time and validate size/checksum before hydration.</li>
<li>Providers stay in control of authorization and storage transport.</li>
</ul>
</div>
</div>

> Reference schema work: <a href="https://github.com/ga4gh/data-repository-service-schemas">GA4GH DRS schemas</a> and the writable DRS prototype branch <a href="https://github.com/ga4gh/data-repository-service-schemas/tree/feature/issue-416-drs-upload">feature/issue-416-drs-upload</a>.

---

## Writable DRS: register, update, and resolve

<div class="columns">
<div>
<svg viewBox="0 0 920 300" width="100%" height="300" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <marker id="arrow2" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
      <path d="M0,0 L0,6 L9,3 z" fill="#1d70a2"/>
    </marker>
  </defs>
  <rect x="28" y="120" width="150" height="80" rx="18" fill="#eaf3fb" stroke="#1d70a2" stroke-width="2"/>
  <text x="103" y="150" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Register</text>
  <text x="103" y="176" text-anchor="middle" font-size="16" fill="#10253f">new object</text>

  <rect x="220" y="82" width="175" height="155" rx="18" fill="#f6f8fb" stroke="#54c6be" stroke-width="2"/>
  <text x="307" y="114" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Writable DRS</text>
  <text x="307" y="146" text-anchor="middle" font-size="16" fill="#10253f">metadata</text>
  <text x="307" y="170" text-anchor="middle" font-size="16" fill="#10253f">checksums</text>
  <text x="307" y="194" text-anchor="middle" font-size="16" fill="#10253f">access methods</text>

  <rect x="442" y="92" width="170" height="136" rx="18" fill="#eaf5f3" stroke="#54c6be" stroke-width="2"/>
  <text x="527" y="128" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Resolve</text>
  <text x="527" y="156" text-anchor="middle" font-size="16" fill="#10253f">access_id</text>
  <text x="527" y="182" text-anchor="middle" font-size="16" fill="#10253f">fresh URL</text>

  <rect x="676" y="120" width="170" height="80" rx="18" fill="#eaf3fb" stroke="#1d70a2" stroke-width="2"/>
  <text x="761" y="150" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Fetch</text>
  <text x="761" y="176" text-anchor="middle" font-size="16" fill="#10253f">verify + hydrate</text>

  <line x1="178" y1="160" x2="220" y2="160" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow2)"/>
  <line x1="395" y1="160" x2="442" y2="160" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow2)"/>
  <line x1="612" y1="160" x2="676" y2="160" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow2)"/>
</svg>
</div>
<div>
<h3>Writable DRS adds lifecycle operations</h3>
<ul>
<li>Register an object with stable identity, checksums, and metadata.</li>
<li>Update description, version, timestamps, or access metadata as the dataset evolves.</li>
<li>Add access methods for HTTPS, GS, Globus, or other provider-backed transport.</li>
<li>Resolve access_id to a fresh URL and verify bytes before materialization.</li>
</ul>
</div>
</div>

---

## Analyst-facing Git repo with DRS-backed local, AnVIL, and Globus data

<div>
<svg viewBox="0 0 980 320" width="100%" height="320" xmlns="http://www.w3.org/2000/svg">
  <defs>
    <marker id="arrow3" markerWidth="10" markerHeight="10" refX="9" refY="3" orient="auto">
      <path d="M0,0 L0,6 L9,3 z" fill="#1d70a2"/>
    </marker>
  </defs>
  <rect x="24" y="120" width="150" height="96" rx="18" fill="#eaf3fb" stroke="#1d70a2" stroke-width="2"/>
  <text x="99" y="156" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Local</text>
  <text x="99" y="184" text-anchor="middle" font-size="16" fill="#10253f">ordinary files</text>

  <rect x="234" y="120" width="180" height="96" rx="18" fill="#f6f8fb" stroke="#54c6be" stroke-width="2"/>
  <text x="324" y="156" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">AnVIL DRS</text>
  <text x="324" y="184" text-anchor="middle" font-size="16" fill="#10253f">provider metadata</text>

  <rect x="478" y="120" width="180" height="96" rx="18" fill="#eaf5f3" stroke="#54c6be" stroke-width="2"/>
  <text x="568" y="156" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Globus</text>
  <text x="568" y="184" text-anchor="middle" font-size="16" fill="#10253f">transfer-backed files</text>

  <rect x="718" y="78" width="220" height="180" rx="20" fill="#f5f7fb" stroke="#1d70a2" stroke-width="2"/>
  <text x="828" y="116" text-anchor="middle" font-size="22" fill="#10253f" font-weight="700">Analyst repo</text>
  <text x="828" y="148" text-anchor="middle" font-size="17" fill="#10253f">Git history</text>
  <text x="828" y="176" text-anchor="middle" font-size="17" fill="#10253f">pointer metadata</text>
  <text x="828" y="204" text-anchor="middle" font-size="17" fill="#10253f">.gitattributes</text>
  <text x="828" y="232" text-anchor="middle" font-size="17" fill="#10253f">selective hydration</text>

  <line x1="174" y1="168" x2="234" y2="168" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow3)"/>
  <line x1="414" y1="168" x2="478" y2="168" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow3)"/>
  <line x1="658" y1="168" x2="718" y2="168" stroke="#1d70a2" stroke-width="4" marker-end="url(#arrow3)"/>
  <path d="M 99 216 C 210 270, 350 270, 828 250" fill="none" stroke="#54c6be" stroke-width="4" stroke-dasharray="8 8" marker-end="url(#arrow3)"/>
  <text x="460" y="275" text-anchor="middle" font-size="17" fill="#1d70a2">data flow: local + DRS + Globus → hydrated analyst workspace</text>
</svg>
</div>

<div class="columns">
<div>
<h3>Repository contents</h3>
<ul>
<li>Local files are tracked as regular Git content.</li>
<li>ANVIL and Globus content are represented as DRS pointers and resolved on demand.</li>
<li>Each user authenticates independently and hydrates only the paths they need.</li>
</ul>
</div>
<div>
<h3>Why it works</h3>
<ul>
<li>Git remains the reviewable source of truth for file paths and history.</li>
<li>DRS remains the authoritative layer for object metadata and access.</li>
<li>Providers retain control over authorization, transport, and cache lifecycle.</li>
</ul>
</div>
</div>

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

## Status: implemented, in flight, and next

<div class="columns3">
<div class="card"><span class="tag">IMPLEMENTED</span><h3>Core DRS + git-drs path</h3><ul><li>Provider-neutral resolver contract</li><li>DRS pointer metadata and checksum validation</li><li>Local + AnVIL + Globus mixed-source repo model</li><li>Access fallback handling and safer credential checks</li></ul></div>
<div class="card"><span class="tag">IN FLIGHT</span><h3>Workflow hardening</h3><ul><li>End-to-end clone/pull acceptance</li><li>Globus collection import and auth flows</li><li>Retry and refresh behavior for expired URLs</li><li>Clean multi-user and denied-user validation</li></ul></div>
<div class="card"><span class="tag">NEXT</span><h3>Production readiness</h3><ul><li>Certify GA4GH schema contract and endpoint compatibility</li><li>Validate upload-oriented writable DRS behavior</li><li>Harden concurrency and diagnostics</li><li>Generalize to other providers and fixtures</li></ul></div>
</div>

| Area | Current state | Evidence |
|---|---|---|
| DRS model | Modernized for writable metadata and live access | GA4GH DRS schema work and writable DRS upload branch |
| git-drs workflow | Validated for mixed local / AnVIL / Globus usage | Provider-neutral resolver, selective hydration, pointer-based Git repo |
| Production proof | Still needs end-to-end acceptance work | Clean-clone, denied-user, and credential-isolation checks pending |

> **Bottom line:** the technical architecture is now in place and the repository model is proven in principle; remaining work is about hardening, contract certification, and acceptance testing.

---

## Decisions needed to start the POC

1. Which authoritative AnVIL resolver contract, endpoint, and OAuth scopes will we certify?
2. What stable test objects cover checksummed, non-checksummed, large, and denied cases?
3. Is the DRS-URI pointer extension compatible with every parser that must read it?
4. Should consumers always run `remote add`, or may a tracked, non-secret bootstrap configure `.git/config`?
5. Who owns the independent-user acceptance environment and compatibility matrix?

### Questions?

Detailed design and acceptance criteria are in [`docs/anvil-terra-poc.md`](anvil-terra-poc.md).
