Git LFS (Large File Storage) **does not natively handle files stored in cloud buckets (like S3, GCS, Azure Blob)** unless additional tools or integrations are introduced.

---

## 🧠 What Git LFS Does

Git LFS is designed to:

- Replace large files in a Git repo with lightweight pointers.
- Store the actual file content on **a Git LFS server**, which is usually:
  - Co-located with Git hosting (e.g., GitHub, GitLab, Bitbucket), or
  - Hosted separately (e.g., custom LFS servers).

When you `git clone` or `git pull`, the Git LFS client fetches the file content from the LFS server.

---

## 🚫 Limitations with Cloud Buckets

Git LFS:

- **Does not automatically recognize or manage files** stored in external cloud buckets like S3, GCS, or Azure.
- **Cannot directly sync or link to files in a cloud bucket** without:
  - Downloading them manually and committing them via LFS.
  - Using custom tooling to bridge Git LFS pointers with cloud bucket contents.

---

## ✅ Possible Workarounds or Integrations

To incorporate cloud bucket storage with Git LFS, users may:

### 1. **Use a custom Git LFS server that backs to S3**
- Example: [lfs-test-server](https://github.com/git-lfs/lfs-test-server) or other S3-backed LFS servers.
- Stores LFS objects in S3, **but still requires Git LFS client operations** for tracking/pulling.

### 2. **Manually sync cloud bucket contents with Git LFS**
- Download from the cloud bucket.
- Use `git lfs track` to commit and push to Git LFS server.
- Duplication risk: The file now exists in both Git LFS and the cloud bucket.

### 3. **Symlink or metadata approaches (not portable)**
- Use `.gitattributes` to track LFS pointer.
- Maintain cloud object metadata (e.g., S3 URI) in the repo.
- Requires external scripts or hooks to resolve and download actual content.

---

## ✅ Summary Table

| Feature                              | Git LFS Support |
|--------------------------------------|------------------|
| Track large files in Git             | ✅ Yes           |
| Native cloud bucket integration (S3, GCS) | ❌ No            |
| Support for S3-backed custom LFS servers | ✅ With setup     |
| Automatically fetch from cloud buckets  | ❌ No            |
| Point LFS to a cloud bucket URI       | ❌ No native support |

---

## 🧩 Missing Feature: Index Remote Files without Download/Upload

To support tracking **remote URLs** (e.g., S3, GCS, etc.) **without downloading or uploading files**, and storing that information in `.lfs-meta/metadata.json`, you can extend the current design as follows:

---


### ✅ Motivation
You want to track and index remote files (e.g., in object stores) using Git + LFS-like semantics, but **without actually downloading or uploading files**. Instead, file metadata is logged, version-controlled, and made available for later workflows like validation or FHIR metadata generation.

---

## 🔄 Updated `.lfs-meta/metadata.json` Format

Now includes a `remote_url` key, replacing the need to add the actual file content to the repo:

```json
{
  "data/foo.vcf": {
    "remote_url": "s3://my-bucket/data/foo.vcf",
    "etag": "abc123etag",
    "size": 12345678,
    "patient": "Patient/1234",
    "specimen": "Specimen/XYZ"
  }
}
```

This file is tracked in Git, but the referenced file is **never downloaded or uploaded**.

---

## 🚀 Updated Usage Workflow

### Step 1: Track a Remote File
```bash
lfs-meta track-remote s3://my-bucket/data/foo.vcf \
  --path data/foo.vcf \
  --patient Patient/1234 \
  --specimen Specimen/XYZ
```

This command:
- Writes to `.lfs-meta/metadata.json`
- Extracts or validates remote file size, ETag, etc. via cloud APIs

### Step 2: Skip `git add data/foo.vcf` — no file is present

Instead, only the metadata is committed:

```bash
git add .lfs-meta/metadata.json
git commit -m "Track remote file foo.vcf without downloading"
```

---

## ⚙️ Updates to `README.md`

You should update the **Usage Workflow** and **metadata.json example** in the README to include:

### 📦 Track Remote File
```bash
lfs-meta track-remote s3://my-bucket/data/foo.vcf \
  --path data/foo.vcf \
  --patient Patient/1234
```

### 📁 Sample `.lfs-meta/metadata.json`
```json
{
  "data/foo.vcf": {
    "remote_url": "s3://my-bucket/data/foo.vcf",
    "etag": "abc123etag",
    "size": 12345678,
    "patient": "Patient/1234"
  }
}
```

### 🧬 Generate FHIR Metadata
```bash
lfs-meta init-meta \
  --input .lfs-meta/metadata.json \
  --output META/DocumentReference.ndjson
```

Generates:
```json
{
  "resourceType": "DocumentReference",
  "content": [
    {
      "attachment": {
        "url": "s3://my-bucket/data/foo.vcf",
        "title": "foo.vcf"
      }
    }
  ],
  "subject": {
    "reference": "Patient/1234"
  }
}
```

---

## ✅ Benefits

| Feature                  | Description |
|--------------------------|-------------|
| ☁️ Remote index only      | Tracks remote data without local storage |
| 📋 Auditable metadata     | Commit metadata to Git without binary bloat |
| 🔄 Interoperable with Gen3| Downstream tools can consume this |
| 🔐 Permissions respected | No direct copy of sensitive files |

---

# Credential management `track-remote` command obtains the credentials it needs to read metadata from remote object stores (e.g., S3, GCS, Azure Blob).

---

## 🔐 Credential Handling for `track-remote`

The `lfs-meta track-remote` command must authenticate to the cloud provider in order to retrieve metadata such as file size, ETag, or content type. This is done **without downloading the file**, using read-only **head/object metadata** APIs.

Supported cloud providers (initial targets):
- ✅ AWS S3
- ✅ Google Cloud Storage (GCS)
- ✅ Azure Blob Storage

### 🔑 Credential Lookup Strategy

The command uses the following order of precedence to locate credentials:

---

### 📦 AWS S3

| Method                     | Description |
|----------------------------|-------------|
| `AWS_PROFILE`              | Use a named profile from `~/.aws/credentials` |
| `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY` | Set directly as environment variables |
| EC2/ECS/IRSA IAM Roles     | Automatically used in cloud environments with role-based access |

> 💡 You can simulate this locally:
```bash
export AWS_PROFILE=aced-research
lfs-meta track-remote s3://my-bucket/data/foo.vcf --path data/foo.vcf
```

---

### 🌍 Google Cloud Storage (GCS)

| Method                       | Description |
|------------------------------|-------------|
| `GOOGLE_APPLICATION_CREDENTIALS` | Path to a service account key JSON |
| gcloud CLI default credentials | Automatically picked up if `gcloud auth application-default login` is used |

> 💡 Example:
```bash
export GOOGLE_APPLICATION_CREDENTIALS=~/gcs-access-key.json
lfs-meta track-remote gs://my-bucket/data/foo.vcf --path data/foo.vcf
```

---

### ☁️ Azure Blob Storage

| Method                       | Description |
|------------------------------|-------------|
| `AZURE_STORAGE_CONNECTION_STRING` | Full connection string for access |
| `AZURE_STORAGE_ACCOUNT` + `AZURE_STORAGE_KEY` | Account name and key variables |
| Azure CLI login               | Supports `az login` if the SDK allows fallback |

---

### 🔧 Fallback

If credentials are not detected automatically, `lfs-meta track-remote` should:
- Display a clear error message
- Suggest how to set environment variables
- Optionally support a `--credentials` flag for custom paths or credential profiles

---

