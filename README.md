# Git DRS

## About

Built off [Git LFS](https://git-lfs.com/), Git DRS allows you to store file contents outside of the Git repo such as in a gen3 bucket, while keeping a pointer to the file inside the repo. With Git DRS, data files that are traditionally too large to store in Git can be tracked along with your code in a single Git repo! And the best part: you can still use the same Git commands you know (and possibly love)! Using just a few extra command line tools, Git DRS helps consolidate your data and code into a single location. 

## Basics

Git DRS functions within Git, so you will only need a few extra commands (`git-lfs pull`, `git-drs init`, etc) that aren't the usual Git commands to do this. Git DRS primarily plugs in the following ways:
- `git add`: during each add, Git LFS processes your file and checks in a pointer to git.
- `git commit`: before each commit, Git DRS creates a DRS object that stores the details of your file needed to push.
- `git push` / `git pull`: before each push, Git DRS handles the transfer of each committed file 
- `git pull`: Git DRS pulls from the DRS server to your working directory if it doesn't already exists locally

## Getting Started: Gen3 DRS Server

### Dependencies

1. Download [Git LFS](https://git-lfs.com/) (`brew install git-lfs` for Mac users)
2. Configure LFS on your machine
    ```
    git lfs install --skip-smudge
    ```
3. Download credentials from your data commons
   1. Login to your data commons
   2. Click your email in the top right to go to your profile
   3. Click Create API Key -> Download JSON
   4. Make note of the path that it downloaded to
4. Download Git DRS
    ```
    # build git-drs from source w/ custom gen3-client dependency
    git clone --recurse-submodule https://github.com/bmeg/git-drs.git
    cd git-drs
    go build

    # make the executable accessible
    export PATH=$PATH:$(pwd)
    ```
5. Clone an existing DRS repo. If you don't already have one set up see "Setting up your repo"
    ```
    cd ..

    # clone test repo
    git clone git@source.ohsu.edu:CBDS/git-drs-test-repo.git
    cd git-drs-test-repo
    ```
6. Configure general acccess to your data commons
    ```
    git drs init --profile <data-commons-name> --apiendpoint https://data-commons-name.com/ --cred /path/to/downloaded/credentials.json
    ```

### Project Setup

When you do `git drs init`, there are a couple things already set up for you...
- `.drs` directory to automatically store any background files and logs needed during execution
- Git settings to sync up the git with gen3 services
- a gen3 profile is created for you so that you can access gen3

In your own repo, all you need to setup is a .drsconfig file. Once you have created a Git repo, create a `.drs/config` with the following structure

```
{
  "gen3Profile": "<gen3-profile-here>",
  "gen3Project": "<project-id-here>",
  "gen3Bucket": "<bucket-name-here>"
}
```
- `gen3Profile` stores the name of the profile you specified in `git drs init` (eg the  `<data-commons-name>` above)
- `gen3Project` is the project ID uniquely describing the data from your project. This will be provided to you by a data commons administrator
- `gen3Bucket` is the name of the bucket that you will be using to store all your files. This will also be provided by a data commons administrator


### Quick Start Commands

**Track Specific File Types**
Store all bam files as a pointer in the Git repo and store actual contents in the DRS server. This is handled by a configuration line in `.gitattributes`. 
```
git lfs track "*.bam"
git add .gitattributes
```

**Pull Files**
Pull a single file
```
git lfs pull -I /path/to/file
```
Pull all non-localized files
```
git lfs pull
```


### When to Use Git vs Git LFS vs Git DRS
The goal of Git DRS is to maximize integration with the Git workflow using a minimal amount of extra tooling. That being said, sometimes `git lfs` commands or `git drs` commands will have to be run outside of the Git workflow. Here's some advice on when to use each of the three...
- **Git DRS**: Only used for initialization of your local repo! The rest of Git DRS is triggered automatically.
- **Git LFS**: Used to interact with files that are tracked by LFS. Examples include
   - `git lfs track` to track files whose contents are stored outside of the Git repo
   - `git lfs ls-files` to get a list of LFS files that LFS tracks
   - `git lfs pull` to pull a file whose contents exist on a server outside of the Git repo.
- **Git**: Everything else! (eg adding/committing files, pushing files, cloning repos, and checking out different commits)

### Troubleshooting

To see more logs and errors, see the log files in the `.drs` directory.

## Implementation Details

### Adding new files
When new files are added, a [precommit hook](https://git-scm.com/book/ms/v2/Customizing-Git-Git-Hooks#:~:text=The%20pre%2Dcommit,on%20new%20methods.) is run which triggers `git drs precommit`. This takes all of the LFS files that have been staged (ie `git add`ed) and creates DRS records for them. Those get used later during a push to register these new files in the DRS server. DRS objects are only created during this pre-commit if they have been staged
and don't already exist on the DRS server.

### File transfers

In order to push file contents to a different system, Git DRS makes use of [custom transfers](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md). These custom transfer are how Git LFS sends information to Git DRS to automatically update the server, passing in the files that have been changed for every each commit that needs to be pushed.. For instance,in the gen3 custom transfer client, we add a indexd record to the DRS server and upload the file to a gen3-registered bucket.  