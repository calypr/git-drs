#!/bin/bash

# strict
set -euo pipefail
# echo commands as they are executed
if [  "${GIT_TRACE:-}" ]; then
  set -x
fi


# Defaults
CREDENTIALS_PATH_DEFAULT="$HOME/.gen3/calypr-dev.json"
PROFILE_DEFAULT="calypr-dev"
PROJECT_DEFAULT="cbds-monorepos"
GIT_REMOTE_DEFAULT="https://github.com/calypr/monorepo.git"
CLEAN_DEFAULT="false"
CLONE_DEFAULT="false"
UPSERT_DEFAULT="false"
BUCKET_DEFAULT="cbds"

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
    --clone=*)
      CLONE="${1#*=}"
      shift
      ;;
    --clone)
      CLONE="true"
      shift
      ;;
    --upsert=*)
      UPSERT="${1#*=}"
      shift
      ;;
    --upsert)
      UPSERT="true"
      shift
      ;;
    --bucket=*)
      BUCKET="${1#*=}"
      shift
      ;;
    --bucket)
      BUCKET="$2"
      shift 2
      ;;
    -h|--help)
      echo "Usage: $0 [--credentials-path PATH] [--profile NAME] [--project NAME] [--clean] [--clone] [--git-remote NAME] [--upsert]" >&2
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
CLONE="${CLONE:-$CLONE_DEFAULT}"
UPSERT="${UPSERT:-$UPSERT_DEFAULT}"
BUCKET="${BUCKET:-$BUCKET_DEFAULT}"

IFS='-' read -r PROGRAM PROJECT <<< "$PROJECT"

export CREDENTIALS_PATH
export PROFILE
export PROGRAM
export PROJECT
export GIT_REMOTE
export CLONE
export UPSERT

echo "Using CREDENTIALS_PATH=$CREDENTIALS_PATH" >&2
echo "Using PROFILE=$PROFILE" >&2
echo "Using PROGRAM=$PROGRAM" >&2
echo "Using PROJECT=$PROJECT" >&2
echo "Using GIT_REMOTE=$GIT_REMOTE" >&2
echo "Using CLEAN=$CLEAN" >&2
echo "Using CLONE=$CLONE" >&2
echo "Using UPSERT=$UPSERT" >&2


if [ "$(basename "$PWD")" != "monorepos" ] || [ "$(basename "$(dirname "$PWD")")" != "tests" ]; then
  echo 'error: must run from tests/monorepos directory' >&2
  exit 1
fi

# Create fixtures (Makefile target does this too)
if [ ! -d "fixtures" ]; then
  echo "fixtures/ not found; please fixtures via make test-monorepos" >&2
  exit 1
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

#SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
#ABS_PATH="$(cd "$SCRIPT_DIR/../.." && pwd -P)"
#
#GIT_DRS_EXE=$ABS_PATH/git-drs
#if [ ! -f "$GIT_DRS_EXE" ]; then
#  echo "error: git-drs executable not found at $GIT_DRS_EXE" >&2
#  exit 1
#fi
#export PATH="$ABS_PATH:$PATH"
echo "Using git-drs from: $(which git-drs)" >&2


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
  rm -rf .git .gitattributes .gitignore ~/.gen3/logs/*.* lfs-console.log lfs-console-aggregate.log commit.log commit-aggregate.log || true
else
  echo "CLEAN flag not set to true; skipping git state cleanup" >&2
fi

if [ "$CLONE" = "true" ]; then
  rm -rf ../clone || true
  mkdir ../clone || true
  cd ../clone
  echo "Cloning remote repository into ../clone" >&2
  # clone into current directory
  if ! git clone "$GIT_REMOTE" .; then
    echo "error: git clone failed" >&2
    exit 1
  fi
  echo "Finished cloning remote repository into `pwd`" >&2
  echo "Verifying contents of TARGET-ALL-P2/sub-directory-1/*file-0001.dat:" >&2
  if ! grep -q 'https://git-lfs.github.com/spec/v1' ./TARGET-ALL-P2/sub-directory-1/*file-0001.dat; then
    echo "error: expected LFS pointer missing in `TARGET-ALL-P2/sub-directory-1/file-0001.dat`" >&2
    exit 1
  fi
  echo "Pulling LFS objects from remote" >&2
  git drs init
  git drs remote add gen3 "$PROFILE" --cred "$CREDENTIALS_PATH"  --bucket $BUCKET --project "$PROGRAM-$PROJECT" --url https://calypr-dev.ohsu.edu
  git lfs pull origin main
  if grep -q 'https://git-lfs.github.com/spec/v1' ./TARGET-ALL-P2/sub-directory-1/*file-0001.dat; then
    echo "error: LFS pointer resolved and data in `TARGET-ALL-P2/sub-directory-1/file-0001.dat`" >&2
    exit 1
  fi
  echo "Clone and LFS pull successful" >&2
  exit 0
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
  git drs remote add gen3 "$PROFILE" --cred "$CREDENTIALS_PATH"  --bucket $BUCKET --project "$PROGRAM-$PROJECT" --url https://calypr-dev.ohsu.edu
  # Set multipart-threshold to 10 (MB) for testing purposes
  # Using a smaller threshold to force a multipart upload for testing
  # default is 500 (MB)
  git config --local lfs.customtransfer.drs.multipart-threshold 10

  # Set multipart-min-chunk-size to 5 (MB) for testing purposes
  # Using a smaller chunk size to will force a large number of parts for testing
  # To test this, you will need to disable data_clients.OptimalChunkSize in code
  # We used this to test a 5GB+ file upload with many parts which causes a minio error
  # git config --local lfs.customtransfer.drs.multipart-min-chunk-size 5

  # Enable upsert for testing purposes, when adding files to indexd, if the object already exists, delete and re-add it
  if [ "$UPSERT" = "true" ]; then
    git config --local lfs.customtransfer.drs.upsert true
    echo "UPSERT is enabled; set lfs.customtransfer.drs.upsert to true" >&2
  else
    echo "UPSERT is disabled; not setting lfs.customtransfer.drs.upsert" >&2
  fi

  # Ensure enable-data-client-logs is present in git config
  if ! git config --list | grep -q -- 'enable-data-client-logs'; then
    echo "error: git config key 'enable-data-client-logs' not found; please set it before running tests" >&2
    exit 1
  fi

  # verify .gitignore does not exist yet
  if [ -f ".gitignore" ]; then
    echo "error: .gitignore unexpected after git drs init" >&2
    exit 1
  fi

  echo "Finished initializing git repository with git-drs in `pwd`" >&2

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

  if [ -z "${GIT_TRACE:-}" ]; then
    echo "For more verbose git output, consider setting the following environment variables before re-running the script:" >&2
    echo "# export GIT_TRACE=1 GIT_TRANSFER_TRACE=1" >&2
  fi
  git push -f origin main 2>&1 | tee lfs-console.log

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
    if [ -z "${GIT_TRACE:-}" ]; then
      echo "For more verbose git output, consider setting the following environment variables before re-running the script:" >&2
      echo "# export GIT_TRACE=1 GIT_TRANSFER_TRACE=1" >&2
    fi
    git push origin main 2>&1 | tee lfs-console.log
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
    git lfs push --dry-run origin main | wc -l >> lfs-console.log
    echo "##########################################" >> lfs-console.log
    cat lfs-console.log >> lfs-console-aggregate.log

    #
    # start testing content and path changes
    #

    # use the first file found in the directory for testing, that will be a single part file
    target_file=$(find "$dir" -type f -name '*file-0001.dat')
    # strip double slashes from path if any
    target_file=${target_file//\/\//\/}
    if [ -z "$target_file" ]; then
      echo "error: no files found in $dir to test content/path changes" >&2
      exit 1
    fi

    original_oid=$(git lfs ls-files -l | awk -v path="$target_file" '$0 ~ (" " path "$") {print $1; exit}')
    if [ -z "$original_oid" ]; then
      echo "error: unable to find LFS OID for $target_file" >&2
      exit 1
    fi

    echo "Testing content change (OID update) for $target_file" >&2
    echo "content change $(date -u +"%Y-%m-%dT%H:%M:%SZ")" >> "$target_file"
    git add "$target_file"
    git commit -m "Update content for $target_file"

    updated_oid=$(git lfs ls-files -l | awk -v path="$target_file" '$0 ~ (" " path "$") {print $1; exit}')
    if [ -z "$updated_oid" ]; then
      echo "error: unable to find updated LFS OID for $target_file" >&2
      exit 1
    fi
    if [ "$original_oid" = "$updated_oid" ]; then
      echo "error: expected OID change for $target_file after content update" >&2
      exit 1
    fi

    target_base=$(basename "$target_file")
    target_dir=$(dirname "$target_file")
    renamed_path="${target_dir}/renamed-${target_base}"
    echo "Testing path change (rename) for $target_file -> $renamed_path" >&2
    git mv "$target_file" "$renamed_path"
    git commit -m "Rename $target_file to $renamed_path"

    renamed_oid=$(git lfs ls-files -l | awk -v path="$renamed_path" '$0 ~ (" " path "$") {print $1; exit}')
    if [ -z "$renamed_oid" ]; then
      echo "error: unable to find LFS OID for renamed path $renamed_path" >&2
      exit 1
    fi
    if [ "$renamed_oid" != "$updated_oid" ]; then
      echo "error: expected same OID after rename for $renamed_path" >&2
      exit 1
    fi
    if git lfs ls-files -l | grep -Fq " $target_file"; then
      echo "error: expected old path $target_file to be absent after rename" >&2
      exit 1
    fi

    git push origin main 2>&1 | tee -a lfs-console.log
    break  # uncomment for one directory at a time testing
  fi
done
