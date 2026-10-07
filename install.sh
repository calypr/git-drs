#!/bin/bash
#
# Utility script to download git-drs from GitHub releases:
# https://github.com/calypr/git-drs/releases/latest

set -e

REPO="calypr/git-drs"
echo "Installing $REPO"

# GitHub API curl wrapper
fetch_github_api() {
    _url="$1"
    if [ -n "$GITHUB_TOKEN" ]; then
        curl -LsS -H "Authorization: token $GITHUB_TOKEN" "$_url"
    else
        curl -LsS "$_url"
    fi
}

# Latest Release URL
get_latest_release_url() {
    echo "https://api.github.com/repos/$REPO/releases/latest"
}

# Tag URL 
get_tag_release_url() {
    echo "https://api.github.com/repos/$REPO/releases/tags/$1"
}

# Optional tag
VERSION_TAG=$1

# Get release URL
RELEASE_URL=""
if [ -z "$VERSION_TAG" ]; then
    RELEASE_URL=$(get_latest_release_url)
    echo "Fetching latest release information..."
    
    if ! command -v curl >/dev/null 2>&1; then
        echo "Error: curl is required but not installed."
        exit 1
    fi

    # API call to get latest tag
    RESPONSE=$(fetch_github_api "$RELEASE_URL" || true)
    
    # Check if we got a valid JSON with tag_name
    if echo "$RESPONSE" | grep -q '"tag_name":'; then
        VERSION_TAG=$(echo "$RESPONSE" | grep '"tag_name":' | head -n 1 | cut -d '"' -f4)
    else
        echo "Error: Failed to fetch the latest release version. You might be rate-limited by the GitHub API."
        if [ -z "$GITHUB_TOKEN" ]; then
            echo "Try setting GITHUB_TOKEN as an environment variable."
        fi
        # If we can't get it from API, it's a fatal error for this mode
        exit 1
    fi
    echo "No version specified, fetching the latest release ($VERSION_TAG)"
else
    RELEASE_URL=$(get_tag_release_url "$VERSION_TAG")
    # Verify the tag exists
    RESPONSE=$(fetch_github_api "$RELEASE_URL" || true)
    if ! echo "$RESPONSE" | grep -q '"tag_name":'; then
         echo "Error: Release tag '$VERSION_TAG' not found."
         exit 1
    fi
fi

# Remove 'v' prefix
VERSION_NUMBER="${VERSION_TAG#v}"

# Determine OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
  ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
  ARCH="arm64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

# Define the filenames
CHECKSUM_FILE="git-drs_${VERSION_NUMBER}_checksums.txt"

# Fetch asset URLs
echo "Fetching download links..."
# RESPONSE is still from the last API call
ASSETS=$(echo "$RESPONSE" | grep "browser_download_url" | cut -d '"' -f 4)

# Download executable + checksums
echo "Downloading git-drs $VERSION_TAG for $OS $ARCH"
DOWNLOADED_TAR=false
DOWNLOADED_CHK=false
TAR_NAME=""

for asset in $ASSETS; do
    # Check for OS-ARCH and tar.gz
    if echo "$asset" | grep -q "${OS}-${ARCH}" && echo "$asset" | grep -q ".tar.gz"; then
        TAR_URL="$asset"
        TAR_NAME=$(basename "$asset")
        echo "Downloading $TAR_NAME..."
        curl -LsS -O "$TAR_URL"
        DOWNLOADED_TAR=true
    # Check for checksums file
    elif echo "$asset" | grep -q "$CHECKSUM_FILE"; then
        echo "Downloading $CHECKSUM_FILE..."
        curl -LsS -O "$asset"
        DOWNLOADED_CHK=true
    fi
done

if [ "$DOWNLOADED_TAR" = false ]; then
    echo "Error: Could not find a release asset for $OS $ARCH"
    exit 1
fi

if [ "$DOWNLOADED_CHK" = true ]; then
    echo "Verifying checksum"
    if ! CHECKSUM_EXPECTED=$(awk -v name="$TAR_NAME" '
        $2 == name { count++; checksum = $1 }
        END {
            if (count == 1 && length(checksum) == 64 && checksum !~ /[^0-9a-fA-F]/)
                print checksum
            else
                exit 1
        }
    ' "$CHECKSUM_FILE"); then
        echo "Error: Missing, duplicate, or invalid checksum for $TAR_NAME." >&2
        exit 1
    fi
    
    # Checksum utility
    if command -v sha256sum >/dev/null 2>&1; then  
        CHECKSUM_ACTUAL=$(sha256sum "$TAR_NAME" | awk '{print $1}')  
    elif command -v shasum >/dev/null 2>&1; then  
        CHECKSUM_ACTUAL=$(shasum -a 256 "$TAR_NAME" | awk '{print $1}')  
    else
        echo "Error: A SHA256 checksum utility (sha256sum or shasum) is required." >&2
        exit 1
    fi

    if [ "$CHECKSUM_EXPECTED" != "$CHECKSUM_ACTUAL" ]; then
        echo "Error: Checksum verification failed for $TAR_NAME."
        exit 1
    fi
else
    echo "Error: Checksum file not found." >&2
    exit 1
fi

# Extract + Install
tar -xzf "$TAR_NAME"

DEST=$2
if [ -z "$DEST" ]; then
    DEST="$HOME/.local/bin"
fi
echo "Installing to $DEST"
mkdir -p "$DEST"
mv git-drs "$DEST/"

# Update PATH in GitHub Actions
if [ -n "$GITHUB_PATH" ]; then
    echo "Updating GITHUB_PATH..."
    echo "$DEST" >> "$GITHUB_PATH"
fi

# Check PATH and update shell config if not in PATH
if ! command -v git-drs >/dev/null 2>&1; then
    # Check if DEST is already in PATH
    if ! echo ":$PATH:" | grep -q ":$DEST:"; then
        echo "Adding $DEST to PATH in shell configuration"

        # Determine shell config file
        if [ -n "$ZSH_VERSION" ]; then
            SHELL_CONFIG="$HOME/.zshrc"
        elif [ -n "$BASH_VERSION" ]; then
            SHELL_CONFIG="$HOME/.bashrc"
        else
            # Default to .bashrc or .profile
            if [ -f "$HOME/.bashrc" ]; then
                SHELL_CONFIG="$HOME/.bashrc"
            else
                SHELL_CONFIG="$HOME/.profile"
            fi
        fi

        echo "export PATH=\$PATH:\"$DEST\"" >> "$SHELL_CONFIG"
        echo "Run 'source $SHELL_CONFIG' to update your current shell session."
    fi
fi

echo "Installation successful!"
echo ""
echo "Run 'git-drs --help' for more info"

# Clean up
if [ -n "$TAR_NAME" ] && [ -f "$TAR_NAME" ]; then
    rm -f "$TAR_NAME"
fi
if [ -f "$CHECKSUM_FILE" ]; then
    rm -f "$CHECKSUM_FILE"
fi
