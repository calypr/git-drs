# Developer Guide

This section is useful for folks who want to learn more of the git DRS internals either as an implementer or as a curious user.

## Adding new files
When new files are added, a [precommit hook](https://git-scm.com/book/ms/v2/Customizing-Git-Git-Hooks#:~:text=The%20pre%2Dcommit,on%20new%20methods.) is run which triggers `git drs precommit`. This takes all of the LFS files that have been staged (ie `git add`ed) and creates DRS records for them. Those get used later during a push to register these new files in the DRS server. DRS objects are only created during this pre-commit if they have been staged and don't already exist on the DRS server.

## File transfers

In order to push file contents to a different system, Git DRS makes use of [custom transfers](https://github.com/git-lfs/git-lfs/blob/main/docs/custom-transfers.md). These custom transfer are how Git LFS sends information to Git DRS to automatically update the server, passing in the files that have been changed for every each commit that needs to be pushed.. For instance,in the gen3 custom transfer client, we add a indexd record to the DRS server and upload the file to a gen3-registered bucket. The same idea applies to the pull and is why we write to a log file instead of directly to stdout during a `git lfs pull` or a `git push`

## Download from source code

If you want to build directly from source code, you will need Go installed...

```sh
# build git-drs from source w/ custom gen3-client dependency
git clone https://github.com/calypr/git-drs.git
cd git-drs
go build

# make the current path of the executable accessible
export PATH=$PATH:$(pwd)
```
