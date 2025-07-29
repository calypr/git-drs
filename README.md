# Git DRS

## About

Built off [Git LFS](https://git-lfs.com/) and [DRS](https://ga4gh.github.io/data-repository-service-schemas/), Git DRS allows you to easily manage data files in a standardized way. With Git DRS, data files that are traditionally too large to store in Git can be tracked along with your code in a single Git repo! We provide standardized access to DRS servers for data platforms like gen3 and AnVIL. And the best part: you can still use the same Git commands you know (and possibly love)! Using just a few extra command line tools, Git DRS helps consolidate your data and code into a single Git workflow.

## Basics

Git DRS functions within Git, so you will only need a few extra commands other than the usual Git commands to manage your files.

Here are some example commands used in pushing a file, detailing the ways in which Git DRS plugs into the Git workflow:

- `git drs init`: Git DRS initializes your repo and server locally
- `git lfs track <file-wildcard>`: Git LFS lets you decide which files should be tracked and stored external to the Git repo.
- `git add <file>`: during each add, Git LFS processes your data file and checks in a pointer to git.
- `git commit`: before each commit, Git DRS creates a DRS object that stores details about your file.
- `git push`: before each push, Git DRS updates the DRS server and transfers each committed file to the configured object storage.


## Getting Started

Currently, we support a couple types of entrypoints to DRS servers:
1. gen3 server on your local machine
2. AnVIL server on a Jupyter environment within Terra
3. AnVIL server on your local machine outside of Terra

Use the setup instructions that match the one you want to get started with.

### Setup

#### Gen3 Setup (General)

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)
2. Configure LFS on your machine
    ```
    git lfs install --skip-smudge
    ```
3. Download credentials from your data commons
   1. Log in to your data commons
   2. Click your email in the top right to go to your profile
   3. Click Create API Key -> Download JSON
   4. Make note of the path that it downloaded to
4. Download Git DRS (Mac example)
   1. Download the Git DRS tar file from the [Releases page](https://github.com/bmeg/git-drs/releases)
   2. cd to the directory where you downloaded the file
   3. Run `tar -xvf git-drs-linux-amd64-0.1.4.tar.gz`
   4. Move the file to a common directory: `mv git-drs /usr/local/bin/`
   5. Make sure the binary is accessible: run `git-drs`
5. Clone an existing Git DRS repo. If you don't have one set up, set one up using GitHub
    ```
    cd ..

    # clone test repo
    git clone <repo-clone-url>.git
    cd <name-of-repo>
    ```
6. Contact your data coordinator to receive the details for your gen3 project, specifically the server url, project ID, and bucket name.
7. Configure general acccess to your data commons. Combined with the credentials path and a profile name of your choice,
    ```
    git drs init --profile <data_commons_name> --url https://datacommons.com/ --cred /path/to/downloaded/credentials.json --project <program-project> --bucket <bucket_name>
    ```


#### AnVIL Setup: Jupyter Environment

To get set up in a Jupyter Environment on Terra,

1. Launch your Jupyter Environment.
2. Upload your Data Explorer manifest to the workspace. Note that all files need sha256 hashes to be uploaded to a git repo
3. Open the terminal session
4. Follow the command line steps below...
```bash
# setup git drs binary
wget https://github.com/calypr/git-drs/releases/download/0.2.0-alpha/git-drs-linux-amd64-0.2.0-alpha.tar.gz
tar -xvf git-drs-linux-amd64-0.2.0-alpha.tar.gz
export PATH="$PATH:$(pwd)"

# setup drs downloader
wget https://github.com/anvilproject/drs_downloader/releases/download/0.1.6-rc.4/drs_downloader
chmod 755 drs_downloader

# confirm binaries are accessible
git-drs --help
drs_downloader --help

# clone and pull files using example repo
git clone https://github.com/quinnwai/super-cool-anvil-analysis.git
cd super-cool-anvil-analysis/
vi .drs/config  # edit the terraProject in the .drsconfig to your Google project ID
git drs init --anvil --terraProject $GOOGLE_PROJECT

# localize the manifest, for example anvil-manifest.tsv
gsutil cp $WORKSPACE_BUCKET/anvil-manifest.tsv .
git drs create-cache anvil-manifest.tsv

# list accessible files (- means not localized, * means localized)
git lfs ls-files

# pull files
git lfs pull -I data_tables_sequencing_dataset.tsv
```

#### AnVIL Setup (General)

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)
2. Configure LFS on your machine
    ```
    git lfs install --skip-smudge
    ```
3. Download Git DRS (Mac example)
   1. Download the Git DRS tar file from the [Releases page](https://github.com/bmeg/git-drs/releases)
   2. cd to the directory where you downloaded the file
   3. Run `tar -xvf git-drs-linux-amd64-0.1.4.tar.gz`
   4. Move the file to a common directory: `mv git-drs /usr/local/bin/`
   5. Make sure the binary is accessible: run `git-drs`
4. Get a Terra project to use for billing
   1. Log in to get to the [AnVIL Workspaces page](https://anvil.terra.bio/#workspaces)
   2. Choose the My Workspace you want to use for billing
   3. Copy the Google Project ID under "CLOUD INFORMATION"
5. Using the Terra project ID, configure general acccess to AnVIL:
    ```
    git drs init --anvil --terraProject <terra-project-id>
    ```

With the setup complete, follow the Quick Start to learn how to do common Git DRS workflows.

### Quick Start
When in doubt, use the `--help` flag to get more info about the commands

#### Track a Specific File Type
Store all bam files as a pointer in the Git repo and store actual contents in the DRS server. This is handled by a configuration line in `.gitattributes`

```
git lfs track "*.bam"
git add .gitattributes
```

#### Example Workflow: Push a File
```
# if the file type is not already being tracked, track the file
git lfs track /path/to/bam
git add .gitattributes

# add the file to git
git add /path/to/file.bam

# check that file.bam is being tracked by LFS
git lfs ls-files

# commit + push!
git commit -m "new bam file"
git push
```

#### Example Workflow: Pull Files
LFS supports pulling via wildcards, directories, and exact paths. Below are some examples...

```
# Pull a single file
git lfs pull -I /path/to/file

# Pull all bams in the top-level directory
git lfs pull -I "*.bam"

# Pull all non-localized files
git lfs pull
```

## When to Use Git vs Git LFS vs Git DRS
The goal of Git DRS is to maximize integration with the Git workflow using a minimal amount of extra tooling. That being said, sometimes `git lfs` commands or `git drs` commands will have to be run outside of the Git workflow. Here's some advice on when to use each of the three...
- **Git DRS**: Only used for initialization of your local repo! The rest of Git DRS is handled in the background automatically.
- **Git LFS**: Used to interact with files that are tracked by LFS. Examples include
   - `git lfs track` to track files whose contents are stored outside of the Git repo
   - `git lfs ls-files` to get a list of LFS files that LFS tracks
   - `git lfs pull` to pull a file whose contents exist on a server outside of the Git repo.
- **Git**: Everything else! (adding/committing files, pushing files, cloning repos, checking out different commits, etc)

## Troubleshooting

- To see more logs and errors, see the log files in the `.drs` directory.
- For errors related to connection like `net/http: TLS handshake timeout`, just try running the command again.
- If you want to try committing again on a set of files, you can undo the last commit using `git reset --hard HEAD~1`. This moves all of the files back into the working directory, so you can retry using `git add` and `git commit`

## Implementation Details

### Adding new files
When new files are added, a [precommit hook](https://git-scm.com/book/ms/v2/Customizing-Git-Git-Hooks#:~:text=The%20pre%2Dcommit,on%20new%20methods.) is run which triggers `git drs precommit`. This takes all of the LFS files that have been staged (ie `git add`ed) and creates DRS records for them. Those get used later during a push to register these new files in the DRS server. DRS objects are only created during this pre-commit if they have been staged
and don't already exist on the DRS server.

### File transfers

In order to push file contents to a different system, Git DRS makes use of [custom transfers](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md). These custom transfer are how Git LFS sends information to Git DRS to automatically update the server, passing in the files that have been changed for every each commit that needs to be pushed.. For instance,in the gen3 custom transfer client, we add a indexd record to the DRS server and upload the file to a gen3-registered bucket.  

### Download from source code
if you want to build directly from source code,
 ```
# build git-drs from source w/ custom gen3-client dependency
git clone --recurse-submodule https://github.com/bmeg/git-drs.git
cd git-drs
go build

# make the executable accessible
export PATH=$PATH:$(pwd)
```
