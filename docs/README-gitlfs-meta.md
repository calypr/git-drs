
---

## 🧩 Overview

**Goal**: Enable this usage:

```bash
git add path/to/file --patient Patient/1234 --specimen Specimen/ABC567
```

…and have the `--patient` and `--specimen` metadata passed through to your **Git LFS custom transfer agent**, such as [`lfs-s3`](https://github.com/nicolas-graves/lfs-s3), for use in metadata handling or cloud-side tagging.

---

## ❗Problem

Git **does not support arbitrary flags on `git add`**.

**Solution**: Use **Git LFS pre-push hooks and custom transfer metadata** to attach additional metadata.

---

### 1. **Capture Extra Metadata Outside `git add`**

Since we can't modify `git add`:

- Track extra metadata in a sidecar file (e.g., `.lfs-meta`)
- Use an extended command like:
  
  ```bash
  git lfs-meta track path/to/file --patient Patient/1234 --specimen Specimen/ABC567
  ```

That command would append this to `.lfs-meta.json`:

```json
{
  "path/to/file": {
    "patient": "Patient/1234",
    "specimen": "Specimen/ABC567"
  }
}
```

---

### 2. **Enhance the Git LFS Transfer Agent**

> Optional: Adding S3 tags or other metadata to the object in S3.

Git LFS passes information to your custom transfer agent (like `lfs-s3`) using stdin/stdout JSON messages.

You can modify `lfs-s3` to:

- Parse the filename it's transferring
- Look up `patient`/`specimen` metadata from `.lfs-meta.json`
- Push that metadata to S3 (e.g., as object tags or upload metadata)

🔧 **Example agent snippet (Go, inside `lfs-s3`)**:

```go
meta := readMetaFile(".lfs-meta.json")
filePath := filepath.Base(obj.Path)

if info, ok := meta[filePath]; ok {
    s3Client.PutObjectTagging(&s3.PutObjectTaggingInput{
        Bucket: aws.String(bucket),
        Key:    aws.String(obj.Oid),
        Tagging: &s3.Tagging{
            TagSet: []*s3.Tag{
                {Key: aws.String("Patient"), Value: aws.String(info.Patient)},
                {Key: aws.String("Specimen"), Value: aws.String(info.Specimen)},
            },
        },
    })
}
```

---

## 📄 Example Workflow

```bash
git lfs track "*.bin"
git add foo.bin

# Add metadata via a companion command
git lfs-meta tag foo.bin --patient Patient/001 --specimen Specimen/XYZ

git commit -m "Add patient-associated data"
git push
```

---

## 🛠 Implementation Notes

| Component        | Description |
|------------------|-------------|
| `.lfs-meta.json` | Project-local map of file path → metadata |
| `git lfs-meta`   | New CLI wrapper to manage sidecar file |
| `lfs-s3`         | Enhanced to load `.lfs-meta.json` and inject metadata during upload |

---

## ✅ Advantages

- No change to Git core or Git LFS binary
- Clean separation of metadata via `.lfs-meta.json`
- Reuses standard Git + LFS behavior
- Fully compatible with custom transfer agents

---

## ⚙️ Configuration: `lfs-meta` Git Integration

### 📦 1. Install the `lfs-meta` Tool

Install globally or per-project. Example (Python-based):

```bash
pip install git-lfs-meta
# or
go install github.com/username/repository@latest
```

Ensure it's in your `$PATH`:

```bash
which lfs-meta
```

---

### 🗂️ 2. Create `.lfs-meta/metadata.json`

In your Git repo:

```bash
mkdir -p .lfs-meta
touch .lfs-meta/metadata.json
```

Track this in your repo:

```bash
echo ".lfs-meta/metadata.json" >> .gitignore
```

Optionally, use `.lfs-meta/.metaignore` to exclude paths from metadata.

---

### 🧩 3. Add `lfs-meta` as a Git subcommand

You can use Git's alias feature:

```bash
git config alias.lfs-meta '!lfs-meta'
```

Now you can run:

```bash
git lfs-meta tag path/to/file --patient Patient/1234
```

---

### 🪝 4. Configure a Git LFS Pre-Push Hook (Optional)

To automatically sync metadata during push, create:

`.git/hooks/pre-push`

```bash
#!/bin/bash
# Hook to prepare metadata for LFS transfer agent

if [ -f ".lfs-meta/metadata.json" ]; then
    echo "[lfs-meta] Metadata file detected."
else
    echo "[lfs-meta] No metadata file present."
fi
```

Make it executable:

```bash
chmod +x .git/hooks/pre-push
```

For more advanced use, this hook could:
- Validate `.lfs-meta/metadata.json`
- Ensure required fields are set before push

---

### 🛠 5. Custom Git Config (optional)

To keep Git aware of `lfs-meta` behavior, configure:

```bash
git config --local lfs.meta.enabled true
git config --local lfs.meta.path .lfs-meta/metadata.json
```

Read these with:

```bash
git config --get lfs.meta.path
```

---

## 🧬 Section: FHIR Metadata Initialization via `lfs-meta`
Extends the `lfs-meta` command to **initialize FHIR metadata** (in the style of `g3t meta init` from the [`gen3_util`](https://github.com/ACED-IDP/gen3_util) project), by reading a sidecar metadata file (like `.lfs-meta/metadata.json`) and generating a `META/DocumentReference.ndjson` file.

### 🎯 Goal

Add a command to `lfs-meta`:

```bash
lfs-meta init-meta
```

This reads `.lfs-meta/metadata.json` and generates a valid FHIR `DocumentReference, Patient, ... ` ndjson file in `META/`.

---

### 📂 Input: `.lfs-meta/metadata.json`

```json
{
  "foo.vcf": {
    "patient": "Patient/1234",
    "specimen": "Specimen/XYZ"
  },
  "bar.pdf": {
    "patient": "Patient/5678"
  }
}
```

---

### 📄 Output: `META/DocumentReference.ndjson, META/Patient.ndjson, ...`

```json
{"resourceType":"DocumentReference","content":[{"attachment":{"url":"s3://bucket/foo.vcf","title":"foo.vcf"}}],"subject":{"reference":"Patient/1234"},"context":{"related":[{"reference":"Specimen/XYZ"}]}}
{"resourceType":"DocumentReference","content":[{"attachment":{"url":"s3://bucket/bar.pdf","title":"bar.pdf"}}],"subject":{"reference":"Patient/5678"}}
```


---

### 🧪 CLI Integration

Add to `lfs-meta` as a subcommand:

```bash
lfs-meta init-meta 
```

Options:
- `--output`: Where to write the `.ndjson`
- `--bucket`: Base URI for constructing FHIR `attachment.url`

---

### 📁 Directory Structure After Init

```
.
├── .lfs-meta/
│   └── metadata.json
├── META/
│   └── DocumentReference.ndjson, etc.
```

---

### ✅ Benefits

| Feature                          | Value                                |
|----------------------------------|--------------------------------------|
| 🔁 Integrates with Gen3 Uploads   | Compatible with `gen3_util` metadata flow |
| 🧬 FHIR-compliant                | Proper `DocumentReference` structure |
| 📦 Reusable                      | Automates metadata for downstream sync tools |

---


## ✅ Resulting Workflow

```bash
git lfs track "*.vcf"
git add foo.vcf

# Associate metadata (via configured alias)
git lfs-meta track foo.vcf --patient Patient/123 --specimen Specimen/ABC

git commit -m "Added foo.vcf with metadata"
git push
```

Absolutely — here’s a **test specification section** for the `lfs-meta` feature that initializes FHIR metadata from a sidecar file, compatible with `gen3_util`.

---

## ✅ Section: Unit and Integration Tests for `lfs-meta init-meta`

---

### 🧪 Unit Test Specifications

| Test Name                                   | Description |
|---------------------------------------------|-------------|
| `test_generate_single_documentreference()`  | Generates a valid FHIR `DocumentReference` for one file |
| `test_generate_with_patient_only()`         | Handles entries that include only the patient reference |
| `test_generate_with_patient_and_specimen()` | Handles entries with both patient and specimen |
| `test_missing_metadata_fields()`            | Gracefully skips or warns on invalid metadata (e.g. missing file or fields) |
| `test_output_is_valid_ndjson()`             | Validates that output is newline-delimited JSON objects |
| `test_bucket_override()`                    | Ensures custom S3 base path is respected |
| `test_empty_metadata()`                     | Outputs nothing (or warns) if input metadata is empty |


---

### 🔁 Integration Test Specifications

| Scenario                                | Setup                                     | Expected Behavior |
|-----------------------------------------|-------------------------------------------|-------------------|
| `test_lfs_meta_end_to_end_minimal()`    | .lfs-meta/metadata.json → `init-meta`     | Produces `META/DocumentReference.ndjson` |
| `test_meta_used_in_gen3_upload()`       | Full Git repo, push to Gen3               | Metadata is accepted by Gen3 API or `g3t upload` |
| `test_multiple_files_ndjson_format()`   | Multiple entries in sidecar               | Multiple NDJSON lines generated |
| `test_script_idempotency()`             | Run `init-meta` twice                     | Output is consistent and append-safe |
| `test_no_metadata_file()`               | No `.lfs-meta/metadata.json`              | Graceful failure or warning message |


---

### 📁 Suggested Test Structure

```
tests/
├── unit/
│   └── test_meta_generation.py
├── integration/
│   └── test_cli_init_meta.py
└── fixtures/
    └── sample_metadata.json
```

