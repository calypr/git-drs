#!/bin/bash
set -e
set -x

# Set repo name and remote URL
REPO_NAME="git-drs-e2e-test"
GIT_USER="cbds"

# REMOTE_URL="https://source.ohsu.edu/$GIT_USER/$REPO_NAME"
REMOTE_URL="git@source.ohsu.edu:$GIT_USER/$REPO_NAME.git"

# Clean up if rerunning (don't fail if not removable)
rm -rf "$REPO_NAME" || true

# Create directory and initialize git
mkdir "$REPO_NAME"
cd "$REPO_NAME"

# Step 1: Initialize Repository
git init
git drs init
git drs remote add gen3 origin --cred ~/.gen3/credentials.json --project cbds-git_drs_test --bucket cbds

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
git commit -m "Initial commit: Add .greeting files with 'hello'"

# Prompt user for remote if not set
git push --set-upstream origin main

# Step 3: Add single duplicate file (edge case)
cp data/A/simple.greeting data/A/duplicate.greeting
git add data/A/duplicate.greeting
git commit -m "Add duplicate .greeting file"
git push

# Step 4: Update and Commit File Changes
echo "A" >> data/A/simple.greeting
echo "B" >> data/B/simple.greeting
echo "C" >> data/C/simple.greeting

git add data/
git commit -m "Update .greeting files with folder-specific greetings"
git push

# Step 5: clone and pull
git clone "$REMOTE_URL"
cd "$REPO_NAME"
git drs init
git drs remote add gen3 origin --cred ~/.gen3/credentials.json --project cbds-git_drs_test --bucket cbds
git lfs pull

# Step 6: test data provenance 
git checkout HEAD~1
git lfs pull
