#!/usr/bin/env bash
# File: `bump-patch.sh`
set -euo pipefail

# Find latest tag excluding major v0
LATEST_TAG=$(git tag --list --sort=-v:refname | head -n1 || true)
if [ -z "$LATEST_TAG" ]; then
  echo "No suitable tag found (excluding v0). Aborting." >&2
  exit 1
fi

# check that the working directory is clean
if [ -n "$(git status --porcelain)" ]; then
  echo "Working directory is not clean. Please commit or stash changes before running this script." >&2
  exit 1
fi

# check coverage timestamp before proceeding, to ensure tests have been run recently
tests/scripts/coverage/assert-coverage-timestamp.sh


usage() {
  cat <<-EOF
Usage: $0 [--major | --minor | --patch]

LATEST_TAG: $LATEST_TAG

Options:
  --major    Bump major (MAJOR+1, MINOR=0, PATCH=0)
  --minor    Bump minor (MINOR+1, PATCH=0)
  --patch    Bump patch (PATCH+1)  [default]
EOF
  exit 1
}

# Parse options
opt_major=false
opt_minor=false
opt_patch=false
count=0

while [ $# -gt 0 ]; do
  case "$1" in
    --major)
      opt_major=true
      count=$((count + 1))
      shift
      ;;
    --minor)
      opt_minor=true
      count=$((count + 1))
      shift
      ;;
    --patch)
      opt_patch=true
      count=$((count + 1))
      shift
      ;;
    --help|-h)
      usage
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage
      ;;
  esac
done

# Default to patch if no option provided
if [ "$count" -eq 0 ]; then
  opt_patch=true
fi

# Disallow specifying more than one
if [ "$count" -gt 1 ]; then
  echo "Specify only one of --major, --minor, or --patch" >&2
  exit 1
fi


# Parse semver vMAJOR.MINOR.PATCH
if [[ "$LATEST_TAG" =~ ^v?([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  MAJOR="${BASH_REMATCH[1]}"
  MINOR="${BASH_REMATCH[2]}"
  PATCH="${BASH_REMATCH[3]}"
else
  echo "Latest tag '$LATEST_TAG' is not in semver format. Aborting." >&2
  exit 1
fi

# Compute new version
if [ "$opt_major" = true ]; then
  NEW_MAJOR=$((MAJOR + 1))
  NEW_MINOR=0
  NEW_PATCH=0
  NEW_TAG="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
  NEW_FILE_VER="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
elif [ "$opt_minor" = true ]; then
  NEW_MAJOR=$MAJOR
  NEW_MINOR=$((MINOR + 1))
  NEW_PATCH=0
  NEW_TAG="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
  NEW_FILE_VER="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
else
  # patch
  NEW_MAJOR=$MAJOR
  NEW_MINOR=$MINOR
  NEW_PATCH=$((PATCH + 1))
  NEW_TAG="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
  NEW_FILE_VER="${NEW_MAJOR}.${NEW_MINOR}.${NEW_PATCH}"
fi

BRANCH="$(git rev-parse --abbrev-ref HEAD)"


echo "Latest branch: $BRANCH"
echo "Latest tag: $LATEST_TAG"
echo "New tag: $NEW_TAG (files will use ${NEW_FILE_VER})"

# Update simple VERSION file if present
if [ -f VERSION ]; then
  echo "${NEW_FILE_VER}" > VERSION
  git add VERSION
fi

## Update version in pyproject.toml or setup.cfg or any file matching version = "x.y.z"
#for f in pyproject.toml setup.cfg $(git grep -Il 'version *= *"' || true); do
#  [ -f "$f" ] || continue
#  # macOS sed in-place
#  sed -E -i '' -e "s/(version *= *\")[^\"]+(\")/\1${NEW_FILE_VER}\2/g" "$f" || true
#  git add "$f"
#done
#
## Update Go internal version file if common pattern exists
#if [ -f internal/version/version.go ]; then
#  sed -E -i '' -e "s/(Version *= *\")[^\"]+(\")/\1${NEW_FILE_VER}\2/" internal/version/version.go
#  git add internal/version/version.go
#fi

# Run tests/builds (non-fatal for Python tests)


# Commit, tag and push
NEW_TAG="v${NEW_TAG}"
git commit -m "chore(release): bump to ${NEW_TAG}" || echo "No changes to commit"
git tag -a "${NEW_TAG}" -m "Release ${NEW_TAG}"

echo "Created tag. Please push tag ${NEW_TAG} on branch ${BRANCH}."
echo "To push, run:"
echo git push origin "${BRANCH}"
echo git push origin "${NEW_TAG}"

