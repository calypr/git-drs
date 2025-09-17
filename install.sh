#!/bin/bash
# Utility script to download git-drs from GitHub releases
# https://github.com/calypr/git-drs/releases/latest

REPO="calypr/git-drs"

echo "Installing git-drs..."

# Function to get the latest release URL if no version is provided
get_latest_release_url() {
    echo "https://api.github.com/repos/$REPO/releases/latest"
}

# Function to get the release URL for a specific tag
get_tag_release_url() {
    echo "https://api.github.com/repos/$REPO/releases/tags/$1"
}

# Parse version tag argument
VERSION_TAG=$1

# Determine the release URL based on whether a version tag was provided
RELEASE_URL=""
if [ -z "$VERSION_TAG" ]; then
    echo "No version specified. Fetching the latest release..."
    RELEASE_URL=$(get_latest_release_url)
else
    echo "Fetching release for version $VERSION_TAG..."
    RELEASE_URL=$(get_tag_release_url $VERSION_TAG)
fi

# Determine OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" == "x86_64" ]; then
  ARCH="amd64"
elif [[ "$ARCH" == "aarch64" || "$ARCH" == "arm64" ]]; then
  ARCH="arm64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

# Define the tar file based on OS and Architecture
TAR_FILE="git-drs-${OS}-${ARCH}*.tar.gz"
CHECKSUM_FILE="git-drs_${VERSION_TAG}_checksums.txt"

# Fetch the release assets URLs
ASSETS=$(curl -s $RELEASE_URL | grep "browser_download_url" | cut -d '"' -f 4)

# Download the tar.gz file and checksums.txt for the detected OS and Arch
echo "Downloading git-drs for $OS $ARCH..."
for asset in $ASSETS; do
    if [[ $asset == *"${OS}-${ARCH}"* && $asset == *".tar.gz"* ]]; then
        TAR_URL=$asset
        TAR_NAME=$(basename $asset)
        curl -LsO $TAR_URL
    elif [[ $asset == *"$CHECKSUM_FILE"* ]]; then
        curl -o $CHECKSUM_FILE -LsO $asset
    fi
done

# Verify checksum
echo "Verifying checksum..."

CHECKSUM_EXPECTED=$(grep $TAR_NAME $CHECKSUM_FILE | awk '{print $1}')

CHECKSUM_ACTUAL=$(sha256sum $TAR_NAME | awk '{print $1}')

if [ "$CHECKSUM_EXPECTED" != "$CHECKSUM_ACTUAL" ]; then
    echo "Checksum verification failed for $TAR_NAME. Exiting..."
    exit 1
fi

# Extract and install the package
echo "Extracting the package..."
tar -xzf $TAR_NAME

# Parse installation destination
DEST=$2

# Determine where to install the git-drs binary
if [ -z "$DEST" ]; then
    DEST=$HOME/.local/bin
fi
echo "Installing git-drs to $DEST..."
mkdir -p $DEST
mv git-drs $DEST

# Verify that git-drs is in the user's PATH
if ! command -v git-drs >/dev/null 2>&1; then
    echo "Adding $DEST to PATH..."
    if [ -n "$ZSH_VERSION" ]; then
        SHELL="$HOME/.zshrc"
    else
        # Default to .bashrc if shell is unknown
        SHELL="$HOME/.bashrc"
    fi

    echo 'export PATH=$PATH:'"$DEST" >> "$SHELL"
    echo "Please restart your terminal or run 'source $SHELL' to update your PATH."
fi

# Clean up
rm $TAR_NAME $CHECKSUM_FILE

echo "Installation successful: $DEST/git-drs"
echo; echo "Run 'git-drs --help' for more info"
