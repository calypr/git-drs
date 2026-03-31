# `run-test.sh`

This file documents the `run-test.sh` helper script located in `tests/monorepos`.

## Purpose
`run-test.sh` prepares and pushes generated fixtures for monorepo tests. It performs the following:

- Ensures the script is run from the `tests/monorepos` directory.
- Generates fixtures via `make test-monorepos` only if `fixtures/` does not exist.
- Verifies that `git-lfs` is installed (attempts `brew install git-lfs` on macOS).
- Uses `git-drs` from `PATH` (prints `which git-drs` at runtime).
- Validates the target Gen3 project exists via `calypr_admin`.
- Initializes a git repo in `fixtures/`, runs `git drs init`, creates or updates `.gitattributes`, configures LFS tracking, and pushes subfolders to the remote.

## setup
Ensure you have the following prerequisites:

```aiignore
# create a test structure in tests/monorepos/fixtures/TARGET-ALL-P2
# see tests/monorepos/create-20MB-test-file.sh, etc. for creating test files
tests/monorepos/fixtures
└── TARGET-ALL-P2
    └── sub-directory-1
        ├── file-0001.dat
        └── file-0003.dat

```
## Invocation
Run from the repository path `tests/monorepos`:

```bash
cd tests/monorepos
./run-test.sh --git-remote https://github.com/your/repo.git
```
Flags may also be provided via equivalent environment variables.


## Flags and environment variables

* --credentials-path / CREDENTIALS_PATH
Path to credentials file. Default: "$HOME/.gen3/calypr-dev.json".


* --profile / PROFILE
Gen3 profile name. Default: calypr-dev.


* --project / PROJECT
Project identifier. Default: cbds-monorepos.
If the value contains a hyphen, the script splits the first hyphen-separated token into PROGRAM and the remainder into PROJECT. Both are exported for downstream use.


* --git-remote / GIT_REMOTE
Remote repository URL for git remote add origin. Default: `https://github.com/calypr/monorepo.git`.

* --max-dirs / MAX_DIRS
Maximum number of top-level fixture directories processed in the push loop.
Default: `0` (process all directories; recommended for realistic coverage).

## Behavior notes

* The script will abort if not run from tests/monorepos.
* Fixtures are generated only when fixtures/ is absent.
* The script uses `git-drs` from `PATH`; ensure your intended binary is first in `PATH`.
* A project existence check is performed with `calypr_admin projects ls --profile "$PROFILE"` and must match `/programs/$PROGRAM/projects/$PROJECT`.
* When initializing the fixtures repo the script:
  * creates or uses branch main,
  * runs git drs init --cred ... --profile ... --bucket calypr --project "$PROGRAM-$PROJECT" ...,
  * ensures .git/drs/config.yaml exists,
  * creates .gitattributes (if missing), commits and pushes,
  * tracks and pushes each top-level fixture subfolder with git LFS

## Troubleshooting

* If git-lfs is missing, install it (the script attempts a brew install git-lfs on macOS).
* Ensure your intended `git-drs` binary is in `PATH` before running tests.
* Ensure the Gen3 project exists before running the script.
