#!/bin/bash
# Utility script to download git-drs from GitHub releases
# https://github.com/calypr/git-drs/releases/latest

REPO="calypr/git-drs"

echo "Installing git-drs"

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
    RELEASE_URL=$(get_latest_release_url)
    VERSION_TAG=$(curl -s $RELEASE_URL | grep '"tag_name":' | cut -d '"' -f4)
    echo "No version specified. Fetching the latest release → $VERSION_TAG"
else
    RELEASE_URL=$(get_tag_release_url $VERSION_TAG)
    echo "Fetching release for version $VERSION_TAG"
fi

# Normalize version tag by removing 'v' prefix if present
# This ensures consistency for file naming
VERSION_NUMBER="${VERSION_TAG#v}"

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
CHECKSUM_FILE="git-drs_${VERSION_NUMBER}_checksums.txt"

# Fetch the release assets URLs
ASSETS=$(curl -s $RELEASE_URL | grep "browser_download_url" | cut -d '"' -f 4)

# Download the tar.gz file and checksums.txt for the detected OS and Arch
echo "Downloading git-drs for $OS $ARCH"
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
echo "Verifying checksum"

CHECKSUM_EXPECTED=$(grep $TAR_NAME $CHECKSUM_FILE | awk '{print $1}')

# Linux
if command -v sha256sum >/dev/null 2>&1; then  
    CHECKSUM_ACTUAL=$(sha256sum $TAR_NAME | awk '{print $1}')  
# macOS
elif command -v shasum >/dev/null 2>&1; then  
    CHECKSUM_ACTUAL=$(shasum -a 256 $TAR_NAME | awk '{print $1}')  
else
    echo "No SHA256 checksum utility found. Please install sha256sum or shasum."
    exit 1
fi

if [ "$CHECKSUM_EXPECTED" != "$CHECKSUM_ACTUAL" ]; then
    echo "Checksum verification failed for $TAR_NAME. Exiting"
    exit 1
fi

# Extract and install the package
echo "Extracting the package"
tar -xzf $TAR_NAME

# Parse installation destination
DEST=$2

# Determine where to install the git-drs binary
if [ -z "$DEST" ]; then
    DEST=$HOME/.local/bin
fi
echo "Installing git-drs to $DEST"
mkdir -p $DEST
mv git-drs $DEST

# Check if git-drs is in the user's PATH
if ! command -v git-drs >/dev/null 2>&1; then

	# Check if $DEST is already in PATH
	if [[ ":$PATH:" != *":$DEST:"* ]]; then
		echo "Adding $DEST to PATH"

		# ZSH
		if [ -n "$ZSH_VERSION" ]; then
			SHELL="$HOME/.zshrc"
		
		# Default to Bash
		else
			SHELL="$HOME/.bashrc"
		fi

		echo 'export PATH=$PATH:'"$DEST" >> "$SHELL"
		echo "Run 'source $SHELL' to update your $PATH."
	fi
fi

# Clean up
rm $TAR_NAME $CHECKSUM_FILE

echo "Installation successful: $DEST/git-drs"
echo; echo "Run 'git-drs --help' for more info"
