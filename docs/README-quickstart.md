# 🧬 CALYPER Project Template with Git LFS Support

This repository provides a template for starting a new CALYPER data project with best practices for:

- Managing large files with [Git LFS](https://git-lfs.github.com)
- Tracking bucket-based object references
- Preventing large files from being accidentally committed to the git repository
- Publishing FHIR-compatible metadata

---

## 🚀 Quickstart

### 1. Clone the Repository

```bash
# TODO 📝: replace with actual template URL
git clone https://github.com/CALYPER-IDP/project-template.git my-project
cd my-project
````
---

### 2. Install Git LFS

Git LFS is required to efficiently manage large binary files.

#### Mac

```bash
brew install git-lfs
```

#### Ubuntu/Debian

```bash
sudo apt-get install git-lfs
```

#### Windows

Download the installer: [git-lfs.github.com](https://git-lfs.github.com)

#### Initialize Git LFS (once per system)

```bash
git lfs install
```

### 3. Install Git-Gen3

This add-on is required to manage metadata and remote data files.

```bash
# TODO 📝: add with actual installation instructions
# This script configures git-lfs, installs git-gen3, and sets up the environment
cd my-project
# macOS or Linux
./scripts/install-git-gen3.sh
# Windows
./scripts/install-git-gen3.bat

```
---

### 4. Track Large Files

#### Track File Extensions

Track specific file types with Git LFS. For example, to track `.bam`, `.ndjson`, and `.csv` files:

```bash
git lfs track "*.bam"
git lfs track "*.ndjson"
git lfs track "*.csv"
```

#### Track Entire Subdirectory

Alternatively, track all files under the `data/` directory:

```bash
git lfs track "data/**"
```

> This pattern will recursively track all files in the `data/` folder, regardless of type.

#### Commit `.gitattributes`

```bash
git add .gitattributes
git commit -m "Track data files and directories with Git LFS"
```


---

### 5. Add Data Files

```bash
mkdir -p data
cp ~/Downloads/mydata.csv data/
git add data/mydata.csv
git commit -m "Add tracked data file"
```

---

### 6. Track Remote Bucket Files (Optional)

Instead of storing the file, use the `add-remote` create a pointer:


Track and commit:

```bash
# TODO 📝: replace with actual `add-remote` command 
git add-remote s3://foo/data/mydata.csv.url 
git commit -m "Track remote bucket object"
```

To enable automatic resolution, configure a [custom transfer agent](#custom-transfer-agent).

---

### 7. 🏷️ Metadata Tagging Files and 🚀 Publishing a Project

Add metadata to files using the `git meta` command. You may tag files with identifier for patient, specimen, or assay

```bash
git meta tag data/foo.vcf --patient Patient/1234 --specimen Specimen/XYZ
```

Generate FHIR Metadata using the `git meta generate` command:

```bash
git meta generate 
```

Validate FHIR Metadata using the `git meta validate` command:

```bash
git meta validate 
```

To customize the metadata, see `TODO` 🔗 Useful FHIR Links

```bash



### 8. Push to Remote Repository

```bash
git push origin main
```

LFS-managed files will be uploaded to the LFS server (or ignored if pointer-only).

---

### 8. Verify Git LFS Files

```bash
git lfs ls-files
```

Expected output:

```
abcdef123456 * data/mydata.csv
```

---

## 📁 What’s Included in This Template

| Path             | Description                                                    |
|------------------|----------------------------------------------------------------|
| `META/`          | FHIR-compatible metadata (e.g. `ResearchStudy.ndjson`)         |
| `.gitattributes` | Git LFS tracking patterns                                      |
| `.git/hooks/`    | Hook scripts, e.g. to prevent large files from being committed |
| `README.md`      | This file                                                      |


---

## 🛠 Troubleshooting

* **Files not tracked by LFS?**
  Run `git lfs track` *before* adding the file.

* **Already committed large file?**
  Use [`git-filter-repo`](https://github.com/newren/git-filter-repo) to rewrite history.

* **Reset Git LFS**:

```bash
git lfs uninstall
git lfs install
```

---

## 🧪 Next Steps

TODO 📝: add with actual next steps
---

