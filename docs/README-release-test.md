# 🧪 `lfs-meta` Pilot Test Script

> Please follow the steps below to test core functionality of the `lfs-meta` tool. Report any issues to the project team via GitHub or the feedback form.

---

## ✅ Prerequisites

Before starting, ensure you have:

- Access to a Git repo cloned from the `lfs-meta` template
- A working installation of the `lfs-meta` CLI
- Access to an S3, GCS, or Azure bucket (read-only)
- Python or Go runtime (depending on implementation)
- A `fence` endpoint (or staging Gen3 system) if testing user sync

---

## 🧭 Part 0 – Track a Local File

### 1.1 Track a local File

```bash
git add data/test.vcf
```

✅ Expected result:
- `.lfs-meta/metadata.json` is updated 


---

## 🧭 Part 1 – Track a Remote File

### 1.1 Track a Remote File

```bash
lfs-meta track-remote s3://my-bucket/path/to/test.vcf \
  --path data/test.vcf \
  --patient Patient/1234 \
  --specimen Specimen/XYZ
```

✅ Expected result:
- `.lfs-meta/metadata.json` is updated with `remote_url`, `size`, `etag`, etc.

---

### 1.2 Commit the Metadata

```bash
git add .lfs-meta/metadata.json
git commit -m "Track remote object test.vcf"
```

✅ Expected result:
- Git diff shows new metadata
- No large file is downloaded or committed

---

## 🧬 Part 2 – Generate FHIR Metadata

### 2.1 Generate `DocumentReference.ndjson`

```bash
lfs-meta init-meta \
  --input .lfs-meta/metadata.json \
  --output META/DocumentReference.ndjson \
  --bucket s3://my-bucket
```

✅ Expected result:
- `META/DocumentReference.ndjson` is created
- File includes `Patient`, `Specimen`, and S3 URL as FHIR attachment

---

### 2.2 Validate the Output

```bash
lfs-meta validate-meta --file META/DocumentReference.ndjson
```

✅ Expected result:
- “Validation passed” message (or warning if required fields are missing)

---

## 👥 Part 3 – Sync User Roles with Gen3

### 3.1 Create Access Config

Create a YAML file at `.access.yaml`:

```yaml
project_id: test-project
roles:
  - username: alice@example.org
    role: submitter
  - username: bob@example.org
    role: reader
```

✅ Expected result:
- YAML is committed to Git and version-controlled

---

### 3.2 Dry-Run the Sync

```bash
lfs-meta sync-users --dry-run --input .access.yaml
```

✅ Expected result:
- Diff is shown: who will be added/removed from Gen3
- No changes are applied

---

### 3.3 Apply the Sync (Optional)

```bash
lfs-meta sync-users --input .access.yaml --confirm
```

✅ Expected result:
- Users are updated in Gen3
- Git commit acts as audit trail

---

## 📋 Part 4 – Submit Feedback

Please provide feedback on:

- 🧠 Was the tool intuitive to use?
- 🧱 Did any commands fail or behave unexpectedly?
- 📝 Were the docs clear and complete?
- 🧪 Any bugs or unexpected behavior?

➡ Submit GitHub Issues or fill out the pilot feedback form:  
**[Feedback Form Link]**

---

## 💡 Optional Tests

- Try with GCS or Azure remote objects
- Test invalid metadata (missing patient/specimen)
- Clone the repo on another machine and repeat the workflow

---