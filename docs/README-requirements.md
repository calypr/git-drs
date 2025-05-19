# Clarifying Requirements

## Requirements
_Inspiration from ([system design interview docs](https://www.hellointerview.com/learn/system-design/in-a-hurry/delivery))_

### Functional Requirements
- Users should be able to **create a new data project**
- Users should be able to **add references to files** that are either **local**, **symlinked**, or **external to the current machine**
- Users should be able to **pull down a subset of the files** from an existing project
- User should be able to **add data files to an existing project**
- User A should be able to **resolve version conflicts** in the project: eg if User B makes changes that aren't on User A's local version
- Users should be able to **add multiple file paths** referring to the same file
- Users should be able to **transfer local files to a remote bucket**
- Users should be able to see their **files updated on the gen3 platform**
- Users should be able to **associate files with other FHIR entities** (patients, specimens, etc)
- Users should be able to **associate files with other FHIR metadata**

### Non-Functional Requirements
- The system should be able to handle over **100k files** in a single project
- The system should be able to handle over **___ TB of data ingestion** for a single project
- The system should be able to handle over **___ TB of data transfer** for a single project
- The system should be 

### Areas of Work
*(adapted from [main README](./README.md#-proposed-modular-architecture)), **bold** for new ones*

1. Project Management (both auth and file version control)
2. **File Transfer** (this is only to upload files)
3. **File Indexing** (this is changing file paths, uploading files, etc)
4. Metadata Management (associate files with entities; tag files with metadata)
5. **Gen3 Integration** (sync git project with gen3 data systems)

### Technical Details based on Requirements

- Users should be able to **create a new data project** as a Git repo
  - an executable containing custom git and gen3 dependencies
  - some setup for a `git <plugin-name> install`
  - way to create repo: only `git clone` [template](./README-gitlfs-template-project.md) for setup?
- Users should be able to **add references to files** that are either **local**, **symlinked**, or **outside of the current machine**
  - All of these references require you to implement a custom clean / smudge so that when data files go into Git's working directory (`.git/`), pointers are created as opposed to non-pointers
  - **clean / smudge for a local file**: file is localized, pointer (hash, filesize, urls) is created from local file
  - **clean for a symlinked file**: file is still local to file,  grabbed from that disk and processed, stored as a pointer how is it stored? 
  - **clean for a external file**:
  - - **<span style="color:red">QW TODO:</span>** still need to add all other combos between clean v smudge and file type
- Users should be able to **pull down a subset of the files** from an existing project
  - Pulling down no files by default (at most only the pointers) (eg `GIT_LFS_SKIP_SMUDGE=1` by default for git lfs)
  - Ability to view and select files that need to be pulled down (**<span style="color:red">QW TODO:</span>** remind me the use case why do we need to pull down files? Why do we need to edit files?)
- User should be able to **add data files to an existing project**
- User A should be able to **resolve version conflicts** in the project: eg if User B makes changes that aren't on User A's local version
- Users should be able to **add multiple file paths** referring to the same file
- Users should be able to **transfer local files to a remote bucket**
- Privileged users should be able to **grant access** of their project **to other users**
- Users should be able to have **different roles** (read only vs read and write vs read write and approve)
- Users should be able to see their **files updated on the gen3 platform**
- Users should be able to **associate files with other FHIR entities** (patients, specimens, etc)
- Users should be able to **associate files with other FHIR metadata**

### User-Facing Design Concerns

- Who is in charge of executing "custom code": whether it should be...
  1. automatically triggered by git hooks (`.git/hooks`)
  2. automatically triggered for specific files (`.gitattributes`)
  3. manually triggered by unique CLI commands (eg `git drs <some-command-here>`)
- How a user interacts with DRS: is DRS the file pointer, an additional metadata store, or something else?
- Expectations of git vs expectations of git drs

### General Design Concerns

- At what level are we interfacing with git? Similarly, at what level are we making use of git lfs? In decreasing order of code reuse, are we...
  1. using git hooks before it gets to git lfs (eg to address the sha limitations *before* it hits git-lfs)
  2. using git lfs extensions to interact with git lfs after the fact (idts, git lfs I think will fail if we don't give it file contents in the file isn't localized case...)
  3. Using only git lfs source code and customizing it at will
- **<span style="color:red">QW TODO:</span>** some of these answers might be in most recent commit ([be0294c](https://github.com/bmeg/git-drs/commit/be0294c1aac7aa74dade90c8166bbf1c5e1066f6))
- For files that "cannot be localized", ie S3 bucket, how are they cleaned and smudged? Updated?
- Project vs program distinction

### Use Cases
- As an OHSU user, I need to transfer files from outside the firewall into OHSU so that they are localized to internal OHSU resources (eg Evotypes)
  - Why not use Globus?
- As an external user, I need to pull down OHSU-internal files so that I can do further processing, downstream analysis, etc on said files (eg Cambridge pulls down OHSU-processed files)
  - This says external user as opposed to OHSU analyst, as a user doesn't need to go through us / gen3 to localize files if it's all internal to OHSU right?
- As an OHSU analyst, I need to index read-only S3 system files to so that ... (eg Jordan multimodal)
- As an OHSU analyst, I need to index files in my directory so that ... (is this a real use case?)
- As an FHIR-aware user, I need to upload FHIR metadata that 
- As a OHSU user, I need to index files that exists on multiple buckets AND make each file downloadable from the right bucket so that I can consolidate my image files in a single project (eg imaging analysis?)

### <span style="color:red">[WIP]</span> User Types on CALIPER

1. **Data steward**: creating data project(s) and ensuring that everything is up to date. Enabling access for folks within their project / program (eg Allison for SMMART datasets)
2. **Data submitter:** adding and editing files to data project, maybe also adding metadata.
3. **Data analyst**: Pulling down relevant files for processing, QA, downstream analysis. Viewing the results of the data project on CALYPR (eg Isabel)

We will assume that our initial users will some level of Git familiarity and computational ability.


------

## Misc

### Example Project (with Use Cases)

1. **Initial file transfer**: I want to use gen3 to transfer my files from a remote server into OHSU premises
2. **Initial file tracking** (Data submitter): I want to create a project and upload files to it. How:
    1. Create a Github repo
    2. Setup of the git drs client (install cli + git hooks)
    3.  of interest
3. **Initial File Upload**: Likely done along with 1, user needs

### Enumerated List of Use Cases

A list of use cases according to the inputs, output, and project states mentioned by the team in the past.

Out-of-scope:
- "multiple inputs": combinatorial inputs of the below (eg pushing local and external files)

### Inputs and Outputs

- **Input**: Input data stored in different locations
    - **Locally stored files within the project directory** (eg Isabel creating bams from fastqs: you have control over the directory where the files are, project dir initialized in a parent directory)
    - **Locally stored files in a shared volume** (eg Jordan /mnt ARC use case, no control over directory where files are)
    - **Externally stored files** in an inaccessible bucket (eg external file management of SMMART files with not access to them)
    - **Locally stored FHIR metadata** (eg SMMART deep FHIR graph queries)
- **Output**: Where to write files to
    - **gen3-registered bucket** (eg: Isabel Evotypes output analysis files shared)
    - **non-gen3-registered bucket** (eg SMMART where we file paths only want to index what’s available to us)
- **[Extra] Project State**: whether project is new or existing
    - **new project** (eg data steward initializing a project from scratch)
    - **existing project** (eg EvoTypes collaborators writing new files onto Evotypes output project)