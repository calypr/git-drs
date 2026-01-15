#!/usr/bin/env bash
set -euo pipefail
set -x # Print each command as it's executed

CREDENTIALS_PATH_DEFAULT="$HOME/.gen3/calypr-dev.json"
PROFILE_DEFAULT="calypr-dev"

# Respect environment variables if set, otherwise use defaults
CREDENTIALS_PATH="${CREDENTIALS_PATH:-$CREDENTIALS_PATH_DEFAULT}"
PROFILE="${PROFILE:-$PROFILE_DEFAULT}"


# Ensure required environment variables are present.
if [ -z "${GH_PAT:-}" ] || [ -z "${GIT_DRS_REMOTE:-}" ]; then
  echo >&2 "Error: required environment variables missing."
  if [ -z "${GH_PAT:-}" ]; then
    echo >&2 "  - GH_PAT is not set"
  fi
  if [ -z "${GIT_DRS_REMOTE:-}" ]; then
    echo >&2 "  - GIT_DRS_REMOTE is not set"
  fi
  exit 1
fi

# Placeholder variables - update before running.
RANDOM_SUFFIX=$(LC_CTYPE=C tr -dc 'A-Za-z' </dev/urandom | head -c6) || true

HOST="https://source.ohsu.edu"
export GH_HOST=source.ohsu.edu
echo gh auth login --hostname $GH_HOST --with-token
echo $GH_PAT | gh auth login --hostname $GH_HOST --with-token

OWNER="CBDS"
REPO_NAME="test-$RANDOM_SUFFIX"
REMOTE_NAME="$GIT_DRS_REMOTE"
TOKEN="$GH_PAT"  # GitHub Personal Access Token with repo scope.
DATA_FILE="data.txt"
DEFAULT_BUCKET="cbds"

REMOTE_URL="${HOST}/${OWNER}/${REPO_NAME}.git"
gh repo create "${OWNER}/${REPO_NAME}" --private

EMAIL="walsbr@ohsu.edu"


mkdir "$REPO_NAME" && pushd "$REPO_NAME"

# Initial repository setup (run inside the new repo directory).
git init

git lfs install --skip-smudge
git drs init -t 16

calypr_admin collaborators add $EMAIL \
  /programs/test/projects/$RANDOM_SUFFIX \
  --project_id $REPO_NAME \
  --profile $PROFILE \
	-w -a

git drs remote add gen3 "$PROFILE" --cred "$CREDENTIALS_PATH"  --bucket $DEFAULT_BUCKET --project $REPO_NAME --url https://calypr-dev.ohsu.edu


git lfs track "*.txt"

git add .gitattributes
git config credential.helper "!f() { echo username=x-oauth-basic; echo password=${TOKEN}; }; f"

git branch -M main

git remote add "${REMOTE_NAME}" "${REMOTE_URL}"

echo $RANDOM_SUFFIX > "${DATA_FILE}"

git add "${DATA_FILE}"

git commit -m "Add test file"

git lfs push --dry-run "${REMOTE_NAME}" main
GIT_TRACE=1 GIT_TRANSFER_TRACE=1  git push --set-upstream "${REMOTE_NAME}" main
echo "Pushed initial commit to remote." $?

# Clone workflow (run in the directory where you want the clone).
git clone "${REMOTE_URL}" cloned-repo

# Change into cloned-repo before running these.
cd cloned-repo
git remote add "${REMOTE_NAME}" "${REMOTE_URL}"
git drs init
git lfs pull "${REMOTE_NAME}" -I "${DATA_FILE}"

grep "$RANDOM_SUFFIX" "${DATA_FILE}" && echo "Data file content verified." || (echo "Data file content mismatch!" && exit 1)

cd  ..
gh repo delete --yes "${OWNER}/${REPO_NAME}"

popd
rm -rf "$REPO_NAME"
