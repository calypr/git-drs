#!/bin/bash
set -e
set -x  # Print each command as it's executed

# Set repo name and remote URL
REPO_NAME="git-drs-e2e-test-bw"
GIT_USER="walsbr"
# REMOTE_URL="https://source.ohsu.edu/$GIT_USER/$REPO_NAME"
REMOTE_URL="git@source.ohsu.edu:$GIT_USER/$REPO_NAME.git"

# Clean up if rerunning (don't fail if not removable)
rm -rf "$REPO_NAME" || true

# Create directory and initialize git
mkdir "$REPO_NAME"
cd "$REPO_NAME"


# Step 1: Initialize Repository
git init
git drs remote add gen3 calypr-dev --url https://calypr-dev.ohsu.edu/ --cred ~/.gen3/calypr-dev.json --project cbds-git_drs_test --bucket cbds
git drs init

# set branch / add remote
git branch -M main
git remote add origin "$REMOTE_URL"

git lfs track "*.greeting"
git add .gitattributes

# Step 2: Create and Commit Initial Files
mkdir -p data/A data/B data/C

DATE="hello $(date)"
echo $DATE > data/A/simple.greeting
echo $DATE > data/B/simple.greeting
echo $DATE > data/C/simple.greeting

git add data/
git add .drs/config.yaml
git commit -m "Initial commit: Add .greeting files with 'hello'"

# Prompt user for remote if not set
# use
GIT_TRACE=1 GIT_TRANSFER_TRACE=1 git push origin main -f
forge publish $GH_PAT

# check that list works
FORGE_UID=$(forge list | sed -n '2p' | awk '{print $2}')
if [ -z "$FORGE_UID" ]; then
  echo "Error: No UID found in forge list"
  exit 1
fi

# check status and output works
sleep 5
forge status "$FORGE_UID" > status.log 2>&1
grep "Status" status.log || { echo "Error: forge status failed"; exit 1; }
forge output "$FORGE_UID" > output.log 2>&1
grep "Logs" output.log || { echo "Error: forge output failed"; exit 1; }


# Keep polling forge status until either Failed or Completed
while true; do
  STATUS=$(forge status "$FORGE_UID" | grep "Status" | awk '{print $6}')
  echo "Current status: $STATUS"
  if [ "$STATUS" == "Completed" ]; then
    echo "Forge job completed successfully."
    break
  elif [ "$STATUS" == "Failed" ]; then
    echo "Forge job failed."
    exit 1
  else
    echo "Forge job still in progress. Waiting for 10 seconds before checking again..."
    sleep 10
  fi
done


# # Step 3: clone and pull files
git clone "$REMOTE_URL"
cd "$REPO_NAME"
git drs init
git lfs pull
