# Git DRS

Built off [Git LFS](https://git-lfs.com/) and [DRS](https://ga4gh.github.io/data-repository-service-schemas/), Git DRS allows you to easily manage data files in a standardized way. With Git DRS, data files that are traditionally too large to store in Git can be tracked along with your code in a single Git repo! And the best part: you can still use the same Git commands you know (and possibly love).

# Basics

| Command                   | Description                                                                                                                               |
|---------------------------|-------------------------------------------------------------------------------------------------------------------------------------------|
| `git drs init`            | Initialize the local repo and server                                                                                                      |
| `git lfs track`           | List the patterns of files that should be tracked                                                                                         |
| `git lfs track <example>` | Track files                                                                                                                               |
| `git lfs untrack`         | Untrack files                                                                                                                             |
| `git add <file>`          | during each add, Git LFS processes your data file and checks in a pointer to git.                                                         |
| `git lfs ls-files`        | List staged files                                                                                                                         |
| `git commit`              | before each commit, Git DRS creates a DRS object that stores details about your file and prepares it for a push.                          |
| `git push`                | before each push, Git DRS updates the DRS server with your file details and uploads each committed file to the configured object storage. |

# Setups

Currently, we support a couple different ways to set up Git DRS depending on where you are doing the setup and what type of DRS server you want to target. Specifically, we have setup for the following:

1. Local user targeting a gen3 DRS server like CALYPR
2. HPC (high-performance computing) user targeting a gen3 DRS server like CALYPR
3. Jupyter environment use within Terra targeting an AnVIL DRS server
4. Local user outside of terra targeting an AnVIL DRS server

Find the setup instructions below that match your use case.

## 1. Local User, Gen3 Server Setup

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)

2. Configure LFS on your machine

   ```sh
   git lfs install --skip-smudge
   ```

3. Download credentials from your data commons:

   1. Log in to your data commons
   2. Click your email in the top right to go to your profile
   3. Click Create API Key -> Download JSON
   4. Make note of the path that it downloaded to

4. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)

5. Install Git DRS using a version from [GitHub Releases](https://github.com/calypr/git-drs/releases)

   ```sh
   bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"
   ```

6. Using the path from the installer output, update your `PATH` variable. For instance, if using bash with Git DRS stored at `$HOME/.local/bin`:
   1. Load up the bash file: `vi ~/.bash_profile`
   2. Add the following line to your Bash profile: `export PATH="$PATH:$HOME/.local/bin"`
   3. Refresh your shell: `source ~/.bash_profile`
7. Confirm that Git DRS is available with `git-drs --help`

## 2. HPC User (eg user on ARC), Gen3 Server Setup

1. Check if Git has been configured already. If not:

   1. On the HPC - Create new SSH key by following the [sections on the Linux tab](https://docs.github.com/en/authentication/connecting-to-github-with-ssh/adding-a-new-ssh-key-to-your-github-account) - “Generating new SSH Key” and “Adding your SSH key to the ssh-agent”

   2. Add this key to the source GitHub at https://source.ohsu.edu/settings/keys

2. Install Git DRS using a version from [GitHub Releases](https://github.com/calypr/git-drs/releases):

   ```sh
   bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"

   git-drs update
   ```

## 3. AnVIL Setup: Jupyter Environment

To get set up in a Jupyter Environment on Terra,

1. Launch your Jupyter Environment.
2. Upload your Data Explorer manifest to the workspace. Note that all files need sha256 hashes to be uploaded to a git repo
3. Open the terminal session
4. Follow the Installation and Running the Executable steps to install [DRS Downloader](https://github.com/anvilproject/drs_downloader?tab=readme-ov-file#installation)
5. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)
6. With Git DRS version in hand, follow the command line steps below...

```sh
# setup git drs binary
bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"

# setup drs downloader
git-drs update

# confirm binaries are accessible
git-drs --help

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

## 4. AnVIL Setup (General)

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)

2. Configure LFS on your machine

   ```sh
   git lfs install --skip-smudge
   ```

3. Identify the release of Git DRS that you want from the [Releases page](https://github.com/calypr/git-drs/releases)

4. Install Git DRS. For example, to install version 0.2.2

   ```sh
   bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)" 
   ```

5. Confirm that Git DRS is available with `git-drs --help`

6. Get a Terra project to use for billing

   1. Log in to get to the [AnVIL Workspaces page](https://anvil.terra.bio/#workspaces)
   2. Choose the My Workspace you want to use for billing
   3. Copy the Google Project ID under "CLOUD INFORMATION"

7. Using the Terra project ID, configure general access to AnVIL:
   - Check that `cat .drs/config.yaml` shows an AnVIL server with an `endpoint` and `terra_project`,
   - If the AnVIL server exists, you're good to go
   - If there is no or an incomplete AnVIL server, contact your data coordinator to receive the details for your gen3 project, specifically the server url, project ID, and bucket name. Then, using the credentials file path (step 3) and Terra project ID (step 5), run

   ```sh
   git drs init --server anvil --terraProject <terra-project-id>
   ```

With the setup complete, follow the Quick Start to learn how to do common Git DRS workflows.

# Quick Start

## Initialize Repo

Every time you create or clone a new Git repo, you have to initialize it with Git DRS.

## Clone an Existing Repo

1. Clone the existing repo

   ```sh
   git clone https://github.com/example/example

   cd example
   ```

2. On your local, download credentials from your data commons (ex: https://calypr-public.ohsu.edu/)

   1. Log in to your data commons
   2. Click your email in the top right to go to your profile
   3. Click "Create API Key" → "Download JSON"
   4. Make note of the path that it downloaded to
   5. If doing Git DRS setup on a separate machine, transfer the credentials file over. For example, to move the file over to ARC: `scp /path/to/credentials.json arc:/home/users/<your-username>`
   6. This credential is valid for 30 days and needs to be redownloaded after that.

3. Confirm that your configuration file has been populated, where the `current_server` is `gen3` and `servers.gen3` contains an endpoint, profile .project ID, and bucket filled out

   ```sh
   git drs list-config
   ```

4. Initialize your user credentials. This must be done before every session.
   ```sh
   git drs init --cred credentials.json
   ```

## Setup from scratch (gen3 server)

1. Create a git repo on GitHub

2. Clone an existing Git DRS repo. If you don't have one set up, before continuing.

   ```sh
   cd ..

   # clone test repo
   git https://github.com/example/example

   cd example
   ```

3. On your local, download credentials from your data commons (ex: https://calypr-public.ohsu.edu/)

   1. Log in to your data commons
   2. Click your email in the top right to go to your profile
   3. Click "Create API Key" → "Download JSON"
   4. Make note of the path that it downloaded to
   5. If doing Git DRS setup on a separate machine, transfer the credentials file over. For example, to move the file over to ARC: `scp credentials.json arc:/home/users/<your-username>`
   6. This credential is valid for 30 days and needs to be redownloaded after that

4. Contact your data coordinator to receive the details for your gen3 project, specifically the website url, project ID, and bucket name.

5. Using the info from steps 3 and 4, configure general access to your data commons.

   ```sh
   git drs init --profile <profile> --url https://calypr-public.ohsu.edu/ --cred credentials.json --project <project_id> --bucket <bucket_name>
   ```

6. Confirm that your configuration file has been populated with the data provided above

   ```sh
   git drs list-config
   ```

## Track a Specific Set of Files

If you want to track a data file in the Git repo, you will need to register that file with Git LFS. This is done by doing a `git lfs track` and then git adding the `.gitattributes` that stores this information.

First see what files are already being tracked

```sh
git lfs track
```

Then, determine whether you want to track a single file, a certain set of files, or a whole folder of files.

To track a single file:

```sh
git lfs track example.txt

git add .gitattributes
```

To track all bam files:

```sh
git lfs track "*.bam"

git add .gitattributes
```

To track all files in a particular directory:

```sh
git lfs track "example/**"

git add .gitattributes
```

Just like Git, only files stored in the repository directory can be added. Once you have tracked a file, you can go about doing the usual Git workflow to stage, commit, and push it to GitHub. An example workflow for this is shown below.

## Example Workflow: Push a File

Below are the steps to push a file once you have localized and `init`ed a Git DRS repo.

```sh
# refresh your access token (done at the start of every session!)
git drs init --cred credentials.json

# confirm that your current server and config file is filled out
git drs list-config

# if the file type is not already being tracked, track the file
git lfs track example/

git add .gitattributes

# check list of tracked files before staging the list
git lfs track

# add the file to git
git add example.bam

# see all files being tracked by LFS in the repo
git lfs ls-files

# check that file.bam is being tracked by LFS
git lfs ls-files -I file.bam

# commit + push!
git commit -m "new bam file"

git push
```

## Example Workflow: Pull Files

LFS supports pulling via wildcards, directories, and exact paths. Below are some examples...

```sh
# refresh your access token (done at the start of every session!)
git drs init --cred credentials.json

# confirm that your current server and config file is filled out
git drs list-config

# Pull a single file
git lfs pull -I example/

# Pull all bams in the top-level directory
git lfs pull -I "*.bam"

# Pull all non-localized files
git lfs pull
```

# 5. Troubleshooting

## When to Use Git vs Git LFS vs Git DRS

The goal of Git DRS is to maximize integration with the Git workflow using a minimal amount of extra tooling. That being said, sometimes `git lfs` commands or `git drs` commands will have to be run outside of the Git workflow. Here's some advice on when to use each of the three...

- **Git DRS**: Only used for initialization of your local repo! The rest of Git DRS is handled in the background automatically.

- **Git LFS**: Used to interact with files that are tracked by LFS. Examples include
  - `git lfs track` to track files whose contents are stored outside of the Git repo
  - `git lfs ls-files` to get a list of LFS files that LFS tracks
  - `git lfs pull` to pull a file whose contents exist on a server outside of the Git repo.

- **Git**: Everything else! (adding/committing files, pushing files, cloning repos, checking out different commits, etc)

## Viewing Logs

- To see more logs during file upload and download, view the log files in the `.drs/` directory.

## Common Error Messages

### `net/http: TLS handshake timeout`

Rerun the command again (these are often network-related errors).

### `Upload error: 503 Service Unavailable error has occurred!`

This is likely because your token used to access the gen3 DRS server has expired. To refresh it, run `git drs init --cred /path/to/credentials.json`.

If the error still persists, then try to download a new credentials file using instructions from [step 2](#clone-an-existing-repo) of the Git repo setup.

## Untrack an LFS file

If you realized you made a typo when doing LFS track, you can use

```sh
git lfs track
```

To see all of the file patterns that are being matched and then

```sh
git lfs untrack <file-pattern>
```

to remove a particular file pattern.

You can confirm that the edits are removed, then staging those changes with

```sh
# confirm the pattern is removed
git lfs track

# stage your changes to the list of LFS file patterns
git add .gitattributes
```

## Undoing a `git commit`

If you want to try committing again on a set of files, you can undo the last commit using `git reset --hard HEAD~1`. This moves all of the files back into the working directory, so you can retry using `git add` and `git commit`

## Undoing a `git add`

In cases where you need to undo your staged changes from a git add

```sh
git status
```

to see all your changes and then

```sh
git restore --staged <file1> <file2> ...
```

to restore any files or directories that you might have already committed.

# Contributors

This project was led by @quinnwai as part of the [Ellrott Lab](https://ellrottlab.org/) at [Knight Cancer Institute](https://www.ohsu.edu/knight-cancer-institute).  

<a href="https://github.com/calypr/git-drs/graphs/contributors">
  <img src="https://contrib.rocks/image?repo=calypr/git-drs" />
</a>

> *Made with [contrib.rocks](https://contrib.rocks).*
