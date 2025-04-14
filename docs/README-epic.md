

# 🚀 Epic: Develop `git-gen3` Tool for Git-Based Gen3 Integration

> Create a Git-native utility to track and synchronize remote object metadata, generate FHIR-compliant metadata, and manage Gen3 access control using `git-sync`.

---

## 🧭 Sprint 0: Architecture Spike

### 🎯 Goal:
De-risk implementation by validating core architectural assumptions and tool compatibility.

### 🔬 Tasks:
| ID     | Task Description                                                      | Est. |
|--------|------------------------------------------------------------------------|------|
| SPK-1  | Prototype `track-remote` to fetch metadata (e.g., ETag, size) from S3/GCS | 1d   |
| SPK-2  | Simulate `.lfs-meta/metadata.json` usage in Git repo + commit/push     | 0.5d |
| SPK-3  | Test `init-meta` to produce `DocumentReference.ndjson` via `g3t`-style logic | 1d   |
| SPK-4  | Validate `git-sync` role mappings and diffs against Gen3 fence API     | 1d   |
| SPK-5  | Evaluate GitHub template DX: hooks, portability, local usage           | 0.5d |

### ✅ Deliverables:
- Prototype CLI for `track-remote`
- Sample `.lfs-meta/metadata.json` and generated `META/DocumentReference.ndjson`
- Credential access matrix (S3, GCS, Azure)
- Feasibility report for Git-driven role syncing via `git-sync`
- Recommendation on proceeding with full implementation

---

## 🧭 Sprint 1: CLI Bootstrapping & Remote File Tracking

### 🎯 Goal:
Create the `git-gen3` CLI structure and implement the ability to track remote cloud objects in Git without downloading them.

### 🔨 Tasks:
| ID   | Task Description                                     | Est. |
|------|------------------------------------------------------|------|
| S1-1 | Scaffold `git-gen3` CLI with Click (Python) or Cobra (Go) | 2d   |
| S1-2 | Implement `track` and `track-remote` subcommands     | 2d   |
| S1-3 | Write to `.lfs-meta/metadata.json`                   | 1d   |
| S1-4 | Support auth with AWS, GCS, Azure (env vars + profiles) | 1d |
| S1-5 | Add `pre-push` hook to validate metadata before push | 1d   |
| S1-6 | Unit tests for `track-remote` and metadata structure | 1d   |

### ✅ Deliverables:
- Functional CLI command: `git-gen3 track-remote s3://...`
- `.lfs-meta/metadata.json` updated and committed in Git
- Git hook active for metadata validation
- CI-ready foundation for next sprint

---

## 🧭 Sprint 2: Metadata Initialization + FHIR Generation

### 🎯 Goal:
Transform `.lfs-meta/metadata.json` entries into Gen3-compatible `DocumentReference.ndjson` metadata using FHIR structure.

### 🔨 Tasks:
| ID   | Task Description                                                   | Est. |
|------|--------------------------------------------------------------------|------|
| S2-1 | Implement `init-meta` to emit `META/DocumentReference.ndjson`     | 2d   |
| S2-2 | Populate FHIR fields: `subject`, `context.related`, `attachment`  | 1d   |
| S2-3 | Create `validate-meta` command to check metadata completeness      | 1d   |
| S2-4 | Write tests for `init-meta` and FHIR formatting                    | 1d   |
| S2-5 | Document schema, CLI usage, and FHIR integration points            | 1d   |

### ✅ Deliverables:
- `git-gen3 init-meta` produces valid FHIR NDJSON
- Tool handles patient/specimen references
- Tests validate output conformance
- Documentation aligns with `g3t upload` workflows

---

## 🧭 Sprint 3: Git-Sync Integration & Access Control

### 🎯 Goal:
Replace `collaborator` and `project-management` with Git-based role assignments using `git-sync` and Gen3 fence APIs.

### 🔨 Tasks:
| ID   | Task Description                                                  | Est. |
|------|-------------------------------------------------------------------|------|
| S3-1 | Integrate `git-sync` YAML/CSV parser into `git-gen3 sync-users`  | 2d   |
| S3-2 | Implement dry-run and apply modes for syncing to Gen3 fence      | 1d   |
| S3-3 | Add change auditing (diff viewer from Git commits)               | 1d   |
| S3-4 | End-to-end test: Git → Gen3 user role propagation                 | 1d   |
| S3-5 | Write user guide and governance documentation                    | 1d   |

### ✅ Deliverables:
- `git-gen3 sync-users` CLI reads Git-tracked access config
- Git diffs capture permission changes over time
- Gen3 access control (via Fence) is synced reliably
- Finalized documentation for institutional onboarding

---

## 📅 Sprint Timeline Summary

| Sprint | Focus                           | Duration | Deliverables                                  |
|--------|----------------------------------|----------|-----------------------------------------------|
| 0      | Architecture validation (spike) | 1 week   | Prototypes + greenlight for implementation    |
| 1      | Remote file tracking            | 2 weeks  | `track-remote`, `.lfs-meta`, validation hooks |
| 2      | Metadata generation (FHIR)      | 2 weeks  | FHIR output, `init-meta`, validation tooling  |
| 3      | Git-based access control        | 2 weeks  | `sync-users`, Git audit trail, Fence sync     |

---

## 🛠 Toolchain

| Purpose               | Tool/Stack                |
|------------------------|---------------------------|
| CLI Language           | Python (Click) or Go (Cobra) |
| Object Store APIs      | boto3 (S3), gcsfs, Azure SDK |
| Metadata Serialization | JSON, FHIR NDJSON         |
| Access Sync            | git-sync + Gen3 Fence      |
| Testing                | `pytest` or `go test`     |
| Docs                   | Markdown, GitHub Pages    |

---

## 🧭 Sprint 4: User Testing, Documentation, and Release Planning

### 🎯 Goal:
Conduct functional and usability testing, finalize user documentation, and prepare for internal/external release of the `git-gen3` tool.

---

### 🔨 Tasks:
| ID   | Task Description                                                             | Est. |
|------|------------------------------------------------------------------------------|------|
| S4-1 | Recruit early adopters from internal teams or pilot projects                | 0.5d |
| S4-2 | Collect and triage feedback via GitHub issues or survey                     | 1d   |
| S4-3 | Perform functional validation of all workflows (track, init-meta, sync)     | 1d   |
| S4-4 | Finalize and polish all CLI command help strings and usage messages         | 0.5d |
| S4-5 | Write end-user guide (markdown or GitHub Pages) with examples and FAQs      | 1d   |
| S4-6 | Create changelog and release notes for v1.0                                 | 0.5d |
| S4-7 | Define release checklist and governance process (e.g., approval flow)       | 0.5d |
| S4-8 | Tag first release, publish GitHub release, optionally register PyPI/Homebrew| 0.5d |

---

### ✅ Deliverables:
- End-user documentation published and linked from the repo
- Feedback collected from test users and incorporated as GitHub issues
- Final `v1.0.0` tag and release notes
- Optional: Package published to PyPI (Python) or Homebrew (Go binary)

---

### 📅 Sprint Timeline Summary (Updated)

| Sprint | Focus                           | Duration | Deliverables                                  |
|--------|----------------------------------|----------|-----------------------------------------------|
| 0      | Architecture validation (spike) | 1 week   | Prototypes + greenlight for implementation    |
| 1      | Remote file tracking            | 2 weeks  | `track-remote`, `.lfs-meta`, validation hooks |
| 2      | Metadata generation (FHIR)      | 2 weeks  | FHIR output, `init-meta`, validation tooling  |
| 3      | Git-based access control        | 2 weeks  | `sync-users`, Git audit trail, Fence sync     |
| 4      | Testing, docs, release planning | 1 week   | Docs, feedback, `v1.0.0` release              |


---
