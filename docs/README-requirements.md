# Clarifying Requirements

## Requirements
_Inspiration from ([system design interview docs](https://www.hellointerview.com/learn/system-design/in-a-hurry/delivery))_

We will assume that our initial users will have some level of Git familiarity and computational ability.

### Functional Requirements
**General**
- Users should be able to use mostly conventional git to handle their repositories and only need a minimal set of non-git commands

**File transfer**
- Users should be able to transfer files from outside the firewall into OHSU systems
- Users should be able to pull a subset of files onto their machine

**File indexing**
- A user should be able to upload a set of data files to a common file repository
- Users should be able to update an existing file repository with new files
- Users should be able to refer to the same file even while it is in multiple file paths
- A user should be able to pull in changes on the repository that another user made

**Metadata / Access Control**
- Users should be able to manage permissions for their projects
- Users should be able to associate data files with metadata about other important entities (patients, specimens, etc)

### Non-Functional Requirements
- The system should be able to handle over **100k files** in a single project
- The system should be able to handle over **___ TB of data ingestion** for a single project
- The system should be able to handle over **___ TB of data transfer** for a single project
- The system should be 
### Categories of Functionality
*(adapted from [main README](./README.md#-proposed-modular-architecture), *unlinked* are new ones)

1. **[Project Management](https://github.com/calypr/git-drs/blob/feature/documentation/docs/README.md#1-project-management-utility)** (both permissions management and project version control)
2. **[File Transfer](https://github.com/calypr/git-drs/blob/feature/documentation/docs/README.md#2-file-transfer-utility)** (upload/download files)
3. ***File Indexing*** (changing file paths, indexing files with pointers, etc)
4. **[Metadata Management](https://github.com/calypr/git-drs/blob/feature/documentation/docs/README.md#3-metadata-management-utility)** (associate files with entities; tag files with metadata)
5. ***Gen3 Integration*** (sync git project with gen3 data systems)

### Use Cases
- As an OHSU user, I need to transfer files from outside the firewall into OHSU so that they are localized to internal OHSU resources (eg Evotypes)
  - Why not use Globus?
- As an external user, I need to pull down OHSU-internal files so that I can do further processing, downstream analysis, etc on said files (eg Cambridge pulls down OHSU-processed files)
  - This says external user as opposed to OHSU analyst, as a user doesn't need to go through us / gen3 to localize files if it's all internal to OHSU right?
- As an OHSU analyst, I need to index read-only S3 system files to so that ... (eg Jordan multimodal)
- As an OHSU analyst, I need to index files in my directory so that ... (is this a real use case?)
- As an FHIR-aware user, I need to upload FHIR metadata that 
- As a OHSU user, I need to index files that exists on multiple buckets AND make each file downloadable from the right bucket so that I can consolidate my image files in a single project (eg imaging analysis?)

### Testing: Inputs and Outputs

- **Data Input**: Input data stored in different locations
    - **Locally stored files within the project directory** (eg Isabel creating bams from fastqs: you have control over the directory where the files are, project dir initialized in a parent directory)
    - **Locally stored files in a shared volume** (eg Jordan /mnt ARC use case, no control over directory where files are)
    - **Externally stored files** in an inaccessible bucket (eg external file management of SMMART files with no access to them)
    - **Locally stored FHIR metadata** (eg SMMART deep FHIR graph queries)
  - **Project State**: whether project is new or existing
    - **new project** (eg data steward initializing a project from scratch)
    - **existing project** (eg Isabel adding analysis files onto an Evotypes project)
- **Output**: Where to write files to
    - **gen3-registered bucket** (eg: Isabel Evotypes output analysis files shared)
    - **no bucket** (eg SMMART where we have file paths but no access / only want to index what’s available to us)

## Comparing LFS-based vs DRS-based design

### Comparison of LFS-based vs DRS-based design
expanded table from [original git-gen3 vs git LFS table](https://github.com/calypr/git-drs/pull/3#issuecomment-2835614773)

Feature | git-gen3 | Git LFS | git-drs
-- | -- | -- | --
Purpose | Manage external document references in research projects (esp. Gen3/DRS/Genomics data) | Manage large binary files directly attached to git repositories | manage external document references using DRS-compliant indexd server
Tracking Method | Metadata about files (e.g., path, etag, MD5, SHA256, multiple remote locations) | LFS pointer files (.gitattributes, .git/lfs/objects) point to large file storage | pointers files like LFS, but DRS ID / subset / entire DRS object
Download on Clone | No automatic download; metadata only on clone. Explicit git drs pull needed to retrieve files. | Automatically downloads necessary objects when needed, or lazily during checkout | no automatic download; only pointers
State Management | Tracks file states: Remote (R), Local (L), Modified (M), Untracked (U), Git-tracked (G) | Files either exist in repo checkout or not; no explicit remote vs. local state tracking | Localizes all project-specific DRS objects; optional download
Adding Files | Add files to metadata index (git drs add), choose between upload, symlink, external S3 or DRS refs. | git lfs track files, then git add to push objects into LFS server (gen3 backend via client side `transport customization`) | git lfs track for certain files
Remote Options | Supports multiple remote backends: Gen3 DRS, S3, local filesystems, others | Client side `transport customization` required to redirect to alternate backends | support multiple remote backends as well
Push Behavior | Push uploads only modified files; unchanged references remain metadata-only | Push uploads any committed LFS objects | push uploads any DRS objects, even if pointing to remote files
Symlink Support | Native symlink references supported (git drs add -l) | No native symlink tracking; must be handled manually | no native symlink support (blocked by git not git lfs)
Flexibility with External Sources | Easy to reference existing DRS URIs, S3 paths, shared file paths | Requires a) large objects to be added locally or b) separate handling for existing references, `transport customization` | only DRS, all file paths are referenced within DRS objects
Intended Usage Domain | Scientific data, genomics workflows, distributed datasets | General-purpose large file versioning (source code, game assets, media files, etc.) | Scientific data, genomics workflows, distributed datasets
Integration with Git Tools | Acts as a git plugin (git drs), not a transparent layer | Fully integrated into Git plumbing; transparent after setup | Ideally, fully integrated, plugin may be required
Maturity & Ecosystem | Early stage, focused on Calypr and Gen3 integrations | Mature, standardized, wide tooling ecosystem | Early stage
Integration with clinical metadata | requires integration | requires integration | requires integration

### Pros and Cons
Since the auth-sync, project upload, and metadata tracking are common problems to solve, I'll list pros and cons more focused on the file indexing and file transfer use cases.

git LFS | git DRS
-- | --
[-] less flexible pointers | [+] pointers are customizable
[+] code is all written, just need to extend it | [-] have to manually copy and edit from source
[+] able to pull in changes from upstream LFS | [-] pulling in updates must be manual
[-] only sha can be used for diffing files | [+] greater control of checksum usage
[-] pulls files by default | [+] can pull only the pointers
[-] not compliant with DRS spec | [+] tool can be refactored to interop with other platforms (eg Terra) using DRS
... | [+] BOTH: enforce a unique pointer for a file
[-] BOTH: need to implement symlinking external of git | ...[-] BOTH: how to manage remote file uploads, especially how to diff, validate, store remote files. As well as integrating it to the user | ...
[-] handlings our

## Common Technical Questions
- **Handling the source of truth**: In the "easy" use case, when a user has no access to gen3/OHSU object stores directly, we populate a git repo, then indexd, then the bucket in that order. What about when a user has access to the underlying bucket and makes updates there? How do we keep the up-to-date?
  - How do we ensure that our file metadata is up to date?
- Diff'ing + file changes: how do we know when a file has changed if multiple checksums are being used? Do we have to validate them each time?
- Determining how to track remote files (+ clarifying what even this use case means)