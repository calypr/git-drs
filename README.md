# Git DRS

## About

Built off [Git LFS](https://git-lfs.com/) and [DRS](https://ga4gh.github.io/data-repository-service-schemas/), Git DRS allows you to easily manage data files in a standardized way. With Git DRS, data files that are traditionally too large to store in Git can be tracked along with your code in a single Git repo! We provide standardized access to DRS servers for data platforms like gen3 and AnVIL. And the best part: you can still use the same Git commands you know (and possibly love). Using just a few extra command line tools, Git DRS helps consolidate your data and code into a single Git workflow.

## Basics

There are two main concepts to understand Git DRS: how **Git LFS tracks** specific files as pointers and how **Git DRS stores** the data files referenced by these pointers.

Git LFS primarily ensures that you can track your files and convert them into pointers when you stage those files with Git. More info on Git LFS can be found [here](https://git-lfs.com/).

Git DRS also functions within the Git workflow by building off Git LFS to manage your files. It plugs in 4 main ways:
1. Git DRS is called *explicitly* when initializing a Git repo
2. Git DRS is called *automatically* when committing new files to the repo
3. Git DRS is called *automatically* when pushing files to the remote repo
4. Git DRS is called *explicitly* when you want to download files onto your local machine

Here are some example commands used in pushing a file:

- `git drs init`: Git DRS prepares your Git repo, project configuration and access to the DRS server.
- `git lfs track`: Git LFS lists the patterns of files that LFS is tracking.
- `git lfs track <file-pattern>`: Git LFS lets you decide which files should be tracked and stored external to the Git repo.
- `git lfs untrack`: Git LFS lets you untrack particular patterns if you made a typo.
- `git add <file>`:  Git LFS processes each data file and checks in a pointer to git.
- `git lfs ls-files`: Git LFS lists the special  staged files that are tracked by LFS
- `git commit`: before each commit, Git DRS creates a DRS object that stores details about your file and prepares it for a push.
- `git push`: before each push, Git DRS register your each of your committed files with the DRS server and uploads them to the configured bucket.
- `git lfs pull`: Git LFS downloads the files to your local Git repo.

Use the `--help` flag when calling the lfs and drs commands for more info.


## Setup
Currently, we support a couple different ways to set up Git DRS depending on where you are doing the setup and what type of DRS server you want to target. Specifically, we have setup for the following:
1. Local user targeting a gen3 DRS server like CALYPR
2. HPC (high-performance computing) user targeting a gen3 DRS server like CALYPR
3. Jupyter environment use within Terra targeting an AnVIL DRS server
4. Local user outside of terra targeting an AnVIL DRS server

Find the setup instructions below that match your use case.

#### Setup: Local user targeting Gen3 server 

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)
2. Configure LFS on your machine
    ```bash
    git lfs install --skip-smudge
    ```
3. Download credentials from your data commons
   1. Log in to your data commons
   2. Click your email in the top right to go to your profile
   3. Click Create API Key -> Download JSON
   4. Make note of the path that it downloaded to
4. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)
5. Install Git DRS using a version from [GitHub Releases](https://github.com/calypr/git-drs/releases). For example, to install version 0.2.2
    ```bash
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)" -- 0.2.2
    ```
6. Using the path from the outputted, update your `PATH` variable. For instance, if using bash with Git DRS stored at `$HOME/.local/bin`:
    1. Load up the bash file: `vi ~/.bash_profile`
    2. Add the following line to your Bash profile: `export PATH="$PATH:$HOME/.local/bin"`
    3. Refresh your shell: `source ~/.bash_profile`
7. Confirm that Git DRS is available with `git-drs --help`
   
#### Setup: HPC User (eg user on ARC) targeting Gen3 server

1. On your HPC, download Git LFS (brew install git-lfs for Mac users)
  ```bash
    # download git-lfs binary
    wget https://github.com/git-lfs/git-lfs/releases/download/v3.7.0/git-lfs-linux-amd64-v3.7.0.tar.gz; tar -xzf git-lfs-linux-amd64-v3.7.0.tar.gz

    # make git-lfs binary accessible to current session
    export PREFIX=$HOME
    ./git-lfs-linux-v3.7.0/install.sh

    # ensure you have a bash_profile, should print the path
    ls -a ~/.bash_profile

    # make git-lfs accessible in all future bash sessions
    echo ‘export PATH="$HOME/bin:$PATH"’ >> ~/.bash_profile
    source ~/.bash_profile

    # install git-lfs
    git lfs version
    git lfs install —-skip-smudge

    # clean up files
    rm git-lfs-linux-amd64-v3.7.0.tar.gz
    rm -r git-lfs-3.7.0/
  ```
1. Check if Git has been configured already. If not:
    1. On the HPC - Create new SSH key by following the [sections on the Linux tab](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account) - “Generating new SSH Key” and “Adding your SSH key to the ssh-agent”
    2. Add this key to the source GitHub at https://source.ohsu.edu/settings/keys 
2. Install Git DRS using a version from [GitHub Releases](https://github.com/calypr/git-drs/releases). For example, to install version 0.2.2
    ```bash
    export GIT_DRS_VERSION=0.2.2
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)" -- $GIT_DRS_VERSION
    ```
3. Using the path from the outputted, update your `PATH` variable. For instance, if using bash with Git DRS stored at `$HOME/.local/bin`:
    1. Load up the bash file: `vi ~/.bash_profile`
    2. Add the following line to your Bash profile: `export PATH="$PATH:$HOME/.local/bin"`
    3. Refresh your shell: `source ~/.bash_profile`
4. Confirm that Git DRS is available with `git-drs --help`


#### Setup: Working in Terra Jupyter Environment

To get set up in a Jupyter Environment on Terra,

1. Launch your Jupyter Environment.
2. Upload your Data Explorer manifest to the workspace. Note that all files need sha256 hashes to be uploaded to a git repo
3. Open the terminal session
4. Follow the Installation and Running the Executable steps to install [DRS Downloader](https://github.com/anvilproject/drs_downloader?tab=readme-ov-file#installation)
5. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)
6. With Git DRS version in hand, follow the command line steps below...
```bash
# setup git drs binary
export GIT_DRS_VERSION=<insert-version-here>
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)" -- $GIT_DRS_VERSION

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
git drs init --server anvil --terraProject $GOOGLE_PROJECT

# localize the manifest, for example anvil-manifest.tsv
gsutil cp $WORKSPACE_BUCKET/anvil-manifest.tsv .
git drs create-cache anvil-manifest.tsv

# list accessible files (- means not localized, * means localized)
git lfs ls-files

# pull files
git lfs pull -I data_tables_sequencing_dataset.tsv
```

#### Setup: Local setup targeting AnVIL server 

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)
2. Configure LFS on your machine
    ```bash
    git lfs install --skip-smudge
    ```
3. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)
4. Install Git DRS. For example, to install version 0.2.2
    ```bash
    export GIT_DRS_VERSION=0.2.2
    /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)" -- $GIT_DRS_VERSION
    ```
5. Confirm that Git DRS is available with `git-drs --help`    
6. Get a Terra project to use for billing
   1. Log in to get to the [AnVIL Workspaces page](https://anvil.terra.bio/#workspaces)
   2. Choose the My Workspace you want to use for billing
   3. Copy the Google Project ID under "CLOUD INFORMATION"
7. Using the Terra project ID, configure general acccess to AnVIL:
    - Check that `git drs list-config` shows an AnVIL server with an `endpoint` and `terra_project`,
    - If the AnVIL server exists, you're good to go
    - If there is no or an incomplete AnVIL server, contact your data coordinator to receive the details for your gen3 project, specifically the server url, project ID, and bucket name. Then, using the credentials file path (step 3) and Terra project ID (step 5), run
      ```bash
        git drs init --server anvil --terraProject <terra-project-id>
      ```

With the setup complete, follow the Quick Start to learn how to do common Git DRS workflows.

## Quick Start
When in doubt, use the `--help` flag to get more info about the commands


### Initialize a Git DRS Repo

Every time you create or clone a new Git repo, you have to initialize it with Git DRS.

#### Clone an existing Git DRS repo (gen3 server)
1. Clone the existing repo
    ```bash
    git clone <repo-clone-url>.git
    cd <name-of-repo>
    ```
2. If you're cloning a repo with an SSH URL (eg git@github.com:myname/myproject.git), add the following to your SSH configuration (on Mac located at `~/.ssh/config`): For example, if you are pushing to github.com, this prevents Git from timing out during a long Git push:
    ```
    Host github.com
        TCPKeepAlive yes
        ServerAliveInterval 30
    ```
3. On your local, download credentials from your data commons (ex: https://calypr-public.ohsu.edu/)
    1. Log in to your data commons
    2. Click your email in the top right to go to your profile
    3. Click "Create API Key" → "Download JSON"
    4. Make note of the path that it downloaded to
    5. If doing Git DRS setup on a separate machine, transfer the credentials file over. For example, to move the file over to ARC: `scp /path/to/credentials.json arc:/home/users/<your-username>`
    6. This credential is valid for 30 days and needs to be redownloaded after that.
 4. Confirm that your configuration file has been populated, where the `current_server` is `gen3` and `servers.gen3` contains an endpoint, profile .project ID, and bucket filled out
    ```bash
        git drs list-config
    ```
5. Initialize your user credentials. This must be done before every session.
    ```bash
        git drs init --cred /path/to/downloaded/credentials.json
    ```


#### Setup from scratch (gen3 server)
1. Create a git repo on GitHub 
2. Clone an existing Git DRS repo. If you don't have one set up, before continuing.
    ```bash
    cd ..

    # clone test repo
    git clone <repo-clone-url>.git
    cd <name-of-repo>
    ```
3. If you're cloning a repo with an SSH URL (eg git@github.com:myname/myproject.git), add the following to your SSH configuration (on Mac located at `~/.ssh/config`): For example, if you are pushing to github.com, this prevents Git from timing out during a long Git push:
    ```
    Host github.com
        TCPKeepAlive yes
        ServerAliveInterval 30
    ```
4. On your local, download credentials from your data commons (ex: https://calypr-public.ohsu.edu/)
    1. Log in to your data commons
    2. Click your email in the top right to go to your profile
    3. Click "Create API Key" → "Download JSON"
    4. Make note of the path that it downloaded to
    5. If doing Git DRS setup on a separate machine, transfer the credentials file over. For example, to move the file over to ARC: `scp /path/to/credentials.json arc:/home/users/<your-username>`
    6. This credential is valid for 30 days and needs to be redownloaded after that
5. Contact your data coordinator to receive the details for your gen3 project, specifically the website url, project ID, and bucket name.
6. Using the info from steps 4 and 5, configure general acccess to your data commons.
      ```bash
        git drs init --profile <data_commons_name> --url https://datacommons.com/ --cred /path/to/downloaded/credentials.json --project <project_id> --bucket <bucket_name>
      ```
7. Confirm that your configuration file has been populated with the data provided above
    ```bash
        git drs list-config
    ```

### Track a Specific Set of Files
If you want to track a data file in the Git repo, you will need to register that file with Git LFS. This is done by doing a `git lfs track` and then git adding the  `.gitattributes` that stores this information.

First see what files are already being tracked
```bash
git lfs track
```

Then, determine whether you want to track a single file, a certain set of files, or a whole folder of files.

To track a single file located at `path/to/file.txt`:
```bash
git lfs track path/to/file.txt
git add .gitattributes
```

To track all bam files:
```bash
git lfs track "*.bam"
git add .gitattributes
```

To track all files in a particular directory:
```bash
# track all files in the data/ directory
git lfs track "data/**"
git add .gitattributes
```

Just like Git, only files stored in the repository directory can be added. Once you have tracked a file, you can go about doing the usual Git workflow to stage, commit, and push it to GitHub. An example workflow for this is shown below.


### Example Workflow: Push a File

Below are the steps to push a file once you have [set up a Git DRS repo](#setup). 
```bash

# confirm that your current server and config file is filled out
git drs list-config

# check list of tracked files
git lfs track

# if the file type is not already being tracked, track the file
git lfs track /path/to/bam
git add .gitattributes

# add the file to git
git add /path/to/file.bam

# see all files being tracked by LFS in the repo
git lfs ls-files

# check that file.bam is being tracked by LFS
git lfs ls-files -I file.bam

# commit + push!
git commit -m "new bam file"
git push
```

### Example Workflow: Pull Files

LFS supports pulling via wildcards, directories, and exact paths. Below are some examples to pull a file once you have [set up a Git DRS repo](#setup).

```bash

# confirm that your current server and config file is filled out
git drs list-config

# Pull a single file
git lfs pull -I /path/to/file

# Pull all bams in the top-level directory
git lfs pull -I "*.bam"

# Pull all non-localized files
git lfs pull
```

## Troubleshooting

### When to Use Git vs Git LFS vs Git DRS
The goal of Git DRS is to maximize integration with the Git workflow using a minimal amount of extra tooling. That being said, sometimes `git lfs` commands or `git drs` commands will have to be run outside of the Git workflow. Here's some advice on when to use each of the three...
- **Git DRS**: Only used for initialization of your local repo! The rest of Git DRS is handled in the background automatically.
- **Git LFS**: Used to interact with files that are tracked by LFS. Examples include
   - `git lfs track` to track files whose contents are stored outside of the Git repo
   - `git lfs ls-files` to get a list of LFS files that LFS tracks
   - `git lfs pull` to pull a file whose contents exist on a server outside of the Git repo.
- **Git**: Everything else! (adding/committing files, pushing files, cloning repos, checking out different commits, etc)

### Viewing Logs
- As mentioned in [Basics](#basics), Git DRS plugs in automatically during the git `commit`, `push`, and `pull`, so logs are most useful when debugging those commands.
- To see more logs during file upload and download, view the log files in the `.drs/` directory.


### Common Error Messages
- For errors related to connection like `net/http: TLS handshake timeout`, just try running the command again. These are often network-related errors.
- For errors like `Upload error: 503 Service Unavailable error has occurred! Please check backend services for more details`: this is likely because your token used to access the gen3 DRS server has expired. To refresh it, run `git drs init --cred /path/to/credentials.json`. If the error still persists, then try to download a new credentials file using instructions from [step 4](#clone-an-existing-git-drs-repo-gen3-server) of the Git repo setup.

### Undoing Your Changes

#### Untrack an LFS file
If you realized you made a typo when doing LFS track, you can use

```bash
git lfs track
```

to see all of the file patterns that are being matched and then

```bash
git lfs untrack <file-pattern>
```

to remove a particular file pattern.

You can confirm that the edits are removed, then staging those changes with
```bash
# confirm the pattern is removed
git lfs track

# stage your changes to the list of LFS file patterns
git add .gitattributes
```

#### Undoing a Commit

If you want to try committing again on a set of files, you can undo the last commit using `git reset --hard HEAD~1`. This moves all of the files back into the working directory, so you can retry using `git add` and `git commit`

#### Undoing a git add

In cases where you need to undo your staged changes from a git add

```bash
git status
```

to see all your changes and then 

```bash
git restore --staged <file1> <file2> ...
```

to restore any files or directories that you might have already committed

## Developer Guide

This section is useful for folks who want to learn more of the git DRS internals either as an implementer or as a curious user.

### Adding new files
When new files are added, a [precommit hook](https://git-scm.com/book/ms/v2/Customizing-Git-Git-Hooks#:~:text=The%20pre%2Dcommit,on%20new%20methods.) is run which triggers `git drs precommit`. This takes all of the LFS files that have been staged (ie `git add`ed) and creates DRS records for them. Those get used later during a push to register these new files in the DRS server. DRS objects are only created during this pre-commit if they have been staged and don't already exist on the DRS server.

### File transfers

In order to push file contents to a different system, Git DRS makes use of [custom transfers](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md). These custom transfer are how Git LFS sends information to Git DRS to automatically update the server, passing in the files that have been changed for every each commit that needs to be pushed.. For instance,in the gen3 custom transfer client, we add a indexd record to the DRS server and upload the file to a gen3-registered bucket. The same idea applies to the pull and is why we write to a log file instead of directly to stdout during a `git lfs pull` or a `git push`

### Download from source code
if you want to build directly from source code, you will need Go installed...

 ```bash
# build git-drs from source w/ custom gen3-client dependency
git clone https://github.com/calypr/git-drs.git
cd git-drs
go build

# make the current path of the executable accessible
export PATH=$PATH:$(pwd)
```
