#!/bin/bash

set -euo pipefail

# Defaults
CREDENTIALS_PATH_DEFAULT="$HOME/.gen3/calypr-dev.json"
PROFILE_DEFAULT="calypr-dev"
PROJECT_DEFAULT="cbds-monorepos"
GIT_REMOTE_DEFAULT="https://github.com/calypr/monorepo.git"
CLEAN_DEFAULT="false"

# Parse optional flags (can also be provided via environment variables)
while [ $# -gt 0 ]; do
  case "$1" in
    --credentials-path=*)
      CREDENTIALS_PATH="${1#*=}"
      shift
      ;;
    --credentials-path)
      CREDENTIALS_PATH="$2"
      shift 2
      ;;
    --profile=*)
      PROFILE="${1#*=}"
      shift
      ;;
    --profile)
      PROFILE="$2"
      shift 2
      ;;
    --project=*)
      PROJECT="${1#*=}"
      shift
      ;;
    --project)
      PROJECT="$2"
      shift 2
      ;;
    --git-remote=*)
      GIT_REMOTE="${1#*=}"
      shift
      ;;
    --git-remote)
      GIT_REMOTE="$2"
      shift 2
      ;;
    --clean=*)
      CLEAN="${1#*=}"
      shift
      ;;
    --clean)
      CLEAN="true"
      shift
      ;;
    -h|--help)
      echo "Usage: $0 [--credentials-path PATH] [--profile NAME] [--project NAME] [--clean] [--git-remote NAME]" >&2
      exit 0
      ;;
    *)
      break
      ;;
  esac
done

# Respect environment variables if set, otherwise use defaults
CREDENTIALS_PATH="${CREDENTIALS_PATH:-$CREDENTIALS_PATH_DEFAULT}"
PROFILE="${PROFILE:-$PROFILE_DEFAULT}"
PROJECT="${PROJECT:-$PROJECT_DEFAULT}"
GIT_REMOTE="${GIT_REMOTE:-$GIT_REMOTE_DEFAULT}"
CLEAN="${CLEAN:-$CLEAN_DEFAULT}"


IFS='-' read -r PROGRAM PROJECT <<< "$PROJECT"

export CREDENTIALS_PATH
export PROFILE
export PROGRAM
export PROJECT
export GIT_REMOTE

echo "Using CREDENTIALS_PATH=$CREDENTIALS_PATH" >&2
echo "Using PROFILE=$PROFILE" >&2
echo "Using PROGRAM=$PROGRAM" >&2
echo "Using PROJECT=$PROJECT" >&2
echo "Using GIT_REMOTE=$GIT_REMOTE" >&2
echo "Using CLEAN=$CLEAN" >&2

if [ "$(basename "$PWD")" != "monorepos" ] || [ "$(basename "$(dirname "$PWD")")" != "tests" ]; then
  echo 'error: must run from tests/monorepos directory' >&2
  exit 1
fi

# Create fixtures (Makefile target does this too)
if [ ! -d "fixtures" ]; then
  echo "fixtures/ not found; creating fixtures via make test-monorepos" >&2
  make test-monorepos
else
  echo "fixtures/ exists; skipping fixture creation" >&2
fi

# ensure git-lfs is installed
if ! command -v git-lfs >/dev/null 2>&1; then
  echo "error: git-lfs is not installed; please install it to proceed" >&2
  # Example install command:
  if [ "$(uname -s)" = "Darwin" ]; then
    if ! command -v brew >/dev/null 2>&1; then
      echo "error: Homebrew is not installed. Please install Homebrew first:" >&2
      echo "  https://brew.sh" >&2
      exit 1
    fi
    echo "installing git-lfs via Homebrew on macOS" >&2
    if ! brew install git-lfs; then
      echo "error: failed to install git-lfs via Homebrew" >&2
      exit 1
    fi
    if ! git lfs install --skip-smudge; then
      echo "error: failed to initialize git-lfs" >&2
      exit 1
    fi
  else
    echo "See installation instructions for your platform:" >&2
    echo "  https://github.com/git-lfs/git-lfs/wiki/Installation" >&2
    exit 1
  fi
fi

# ensure git-drs is running from this project's build
# run `which git-drs` and check if it's in the build directory

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
ABS_PATH="$(cd "$SCRIPT_DIR/../.." && pwd -P)"

GIT_DRS_EXE=$ABS_PATH/git-drs
if [ ! -f "$GIT_DRS_EXE" ]; then
  echo "error: git-drs executable not found at $GIT_DRS_EXE" >&2
  exit 1
fi
export PATH="$ABS_PATH:$PATH"
echo "Using git-drs from: $(which git-drs)" >&2

# echo commands as they are executed
set -x

# ensure a gen3 project exists
calypr_admin projects ls --profile "$PROFILE" | grep "/programs/$PROGRAM/projects/$PROJECT" >/dev/null 2>&1 || {
  echo "error: /programs/$PROGRAM/projects/$PROJECT does not exist; please create it first" >&2
  exit 1
}

# Initialize a git repo for the generated fixtures
cd fixtures
echo "Running in `pwd`"  >&2
# to reset git state
if [ "$CLEAN" = "true" ]; then
  echo "Cleaning existing git state" >&2
  rm -rf .git .drs .gitattributes  ~/.gen3/logs/*.* lfs-console.log lfs-console-aggregate.log commit.log commit-aggregate.log
else
  echo "CLEAN flag not set to true; skipping git state cleanup" >&2
fi

# init git repo if not already a git repo
if [ -d .git ]; then
  echo "Git repository already initialized; skipping git init" >&2
else
  echo "Initializing new git repository" >&2

  # use 'main' as default branch name
  git init -b main

  # Add remote origin
  git remote add origin "$GIT_REMOTE"

  # Initialize drs configuration for this repo
  git drs init -t 16
  git drs remote add gen3 "$PROFILE" --cred "$CREDENTIALS_PATH"  --bucket cbds --project "$PROGRAM-$PROJECT" --url https://calypr-dev.ohsu.edu

  # verify fixtures/.drs/config.yaml exists
  if [ ! -f ".drs/config.yaml" ]; then
    echo "error: .drs/config.yaml not found after git drs init" >&2
    exit 1
  fi

  echo "Finished initializing git repository with git-drs in `pwd`" >&2
  git add .drs
  git commit -m "Add .drs" .drs
  git add .gitignore
  git commit -m "Add .gitignore" .gitignore

  # Create an empty .gitattributes file
  # if .gitattributes does not already exist initialize it
  if [ -f .gitattributes ]; then
    echo ".gitattributes already exists; skipping creation" >&2
  else
    echo "Creating empty .gitattributes file" >&2
    touch .gitattributes
    git add .gitattributes
    git commit -m "Add .gitattributes" .gitattributes
  fi
  echo "Finished init.  Force pushing to remote." >&2
  git remote -v

  GIT_TRACE=1 GIT_TRANSFER_TRACE=1  git push -f 2>&1 | tee lfs-console.log

  echo "Finished init.  Finished pushing to remote." >&2
  exit 0

fi

echo "Starting to add subfolders to git LFS tracking" >&2
# For every subfolder in fixtures/, add, commit, and push to remote
for dir in */ ; do
  if [ -d "$dir" ]; then
    # if $dir already in .gitattributes, assume it's already tracked
    if grep -q "^$dir" .gitattributes; then
      echo "$dir is already tracked; skipping"
      continue
    fi
    # $dir has trailing slash; don't need trailing slash in track
    git lfs track "$dir**"
    git add "$dir"
    git commit -am "Add $dir" 2>&1 | tee commit.log
    cat commit.log >> commit-aggregate.log
    GIT_TRACE=1 GIT_TRANSFER_TRACE=1  git push 2>&1 | tee lfs-console.log
    echo "##########################################" >> lfs-console.log
    echo "# finished pushing $dir to remote." >> lfs-console.log
    # if .drs/lfs/objects exists, log last 3 lines of tree
    if [ ! -d ".drs/lfs/objects" ]; then
      echo "# .drs/lfs/objects does not exist." >> lfs-console.log
      echo "##########################################" >> lfs-console.log
    else
      echo "# Last 3 lines of .drs/lfs/objects tree:" >> lfs-console.log
      tree .drs/lfs/objects | tail -3 >> lfs-console.log
    fi
    echo "# git lfs status:" >> lfs-console.log
    git lfs status >> lfs-console.log
    echo "# Number of LFS files to be pushed in dry-run:" >> lfs-console.log
    git lfs push --dry-run | wc -l >> lfs-console.log
    echo "##########################################" >> lfs-console.log
    cat lfs-console.log >> lfs-console-aggregate.log
    break  # uncomment for one directory at a time testing
  fi
done