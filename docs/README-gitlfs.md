# git-lfs-meta-template

> **Template Repository**
>
> This is a GitHub **template repository**. Click **"Use this template"** on GitHub to bootstrap a new project with Git LFS support, metadata tracking, and FHIR integration.

A Git project archetype for managing large files with Git LFS + S3 and synchronizing metadata with FHIR DocumentReferences, supporting integration with Gen3 via `g3t`. Tool-agnostic: the `lfs-meta` utility can be written in **Python**, **Go**, or any language that conforms to expected input/output behavior.

---

## 🌐 Project Layout

```bash
git-lfs-meta-template/
├── .gitignore
├── .gitattributes
├── .lfs-meta/
│   └── metadata.json
├── META/
│   └── DocumentReference.ndjson
│   └── Patient.ndjson, etc...
├── hooks/
│   └── pre-push
├── lfs_meta/           # Optional directory for your implementation
│   └── ...
├── tests/
│   ├── unit/
│   └── integration/
├── requirements.txt    # If Python is used
├── go.mod              # If Go is used
└── README.md
```

---

## 🚀 Getting Started

### 1. Use This Template on GitHub

1. Go to the repository page on GitHub.
2. Click the green **"Use this template"** button.
3. Create a new repository from this template.

Alternatively, clone the project manually:

```bash
git clone https://github.com/YOUR_ORG/git-lfs-meta-template.git
cd git-lfs-meta-template
```

### 2. Install the `lfs-meta` Tool
Install the `lfs-meta` tool in your preferred language:

#### Python
```bash
pip install -e .  # assumes setup.py or pyproject.toml exists
```

#### Go
```bash
go build -o lfs-meta ./cmd/lfs-meta
mv lfs-meta /usr/local/bin/
```

Ensure it's available on your `$PATH`:
```bash
which lfs-meta
```

---

## ⚖️ Git LFS Setup
```bash
git lfs install
git lfs track "*.bin"
echo "*.bin filter=lfs diff=lfs merge=lfs -text" >> .gitattributes
```

---

## ⚙️ Configure Git Hooks

Enable the `pre-push` hook:
```bash
chmod +x hooks/pre-push
ln -s ../../hooks/pre-push .git/hooks/pre-push
```

This will validate `.lfs-meta/metadata.json` before push.

---

## ⚡ Usage Workflow

### Add a Large File
```bash
git add data/foo.vcf
git commit -m "Add file"
```

### Track Metadata
```bash
lfs-meta track data/foo.vcf --patient Patient/1234 --specimen Specimen/XYZ
```

### Generate FHIR Metadata
```bash
lfs-meta init-meta --output META/DocumentReference.ndjson --bucket s3://my-bucket
```

---

## ✅ Tests

Run all tests:
```bash
pytest tests/  # If Python is used
# or
go test ./...  # If Go is used
```

---

## 🌿 Example .lfs-meta/metadata.json
```json
{
  "data/foo.vcf": {
    "patient": "Patient/1234",
    "specimen": "Specimen/XYZ"
  }
}
```

---

## 🔹 Pre-Push Hook
```bash
#!/bin/bash
# hooks/pre-push

if [ ! -f ".lfs-meta/metadata.json" ]; then
  echo "[lfs-meta] No metadata file found. Skipping."
  exit 0
fi

lfs-meta validate --file .lfs-meta/metadata.json || {
  echo "[lfs-meta] Metadata validation failed"
  exit 1
}
```

---

## 🏆 Credits
- Inspired by `g3t meta init` (ACED-IDP)
- Custom LFS support with S3 via [lfs-s3](https://github.com/nicolas-graves/lfs-s3)

---

## ✨ Future Extensions
- Auto-tag S3 objects with metadata
- Metadata schema validation

---
