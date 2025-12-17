# Installation Guide

This guide covers installation of Git DRS across different environments and target DRS servers.

## Prerequisites

All installations require [Git LFS](https://git-lfs.com/) to be installed first:

```bash
# macOS
brew install git-lfs

# Linux (download binary)
wget https://github.com/git-lfs/git-lfs/releases/download/v3.7.0/git-lfs-linux-amd64-v3.7.0.tar.gz
tar -xvf git-lfs-linux-amd64-v3.7.0.tar.gz
export PREFIX=$HOME
./git-lfs-v3.7.0/install.sh

# Configure LFS
git lfs install --skip-smudge
```

## Local Installation (Gen3 Server)

**Target Environment**: Local development machine targeting Gen3 data commons (e.g., CALYPR)

### Steps

1. **Install Git DRS**
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"
   ```

2. **Update PATH**
   ```bash
   # Add to ~/.bash_profile or ~/.zshrc
   export PATH="$PATH:$HOME/.local/bin"
   source ~/.bash_profile  # or source ~/.zshrc
   ```

3. **Verify Installation**
   ```bash
   git-drs --help
   ```

4. **Get Credentials**
   - Log in to your data commons (e.g., https://calypr-public.ohsu.edu/)
   - Click your email → Profile → Create API Key → Download JSON
   - Note the download path for later configuration

## HPC Installation (Gen3 Server)

**Target Environment**: High-performance computing systems targeting Gen3 servers

### Steps

1. **Install Git LFS on HPC**
   ```bash
   # Download and install Git LFS
   wget https://github.com/git-lfs/git-lfs/releases/download/v3.7.1/git-lfs-linux-amd64-v3.7.1.tar.gz
   tar -xvf git-lfs-linux-amd64-v3.7.1.tar.gz
   export PREFIX=$HOME
   ./git-lfs-3.7.1/install.sh
   
   # Make permanent
   echo 'export PATH="$HOME/bin:$PATH"' >> ~/.bash_profile
   source ~/.bash_profile
   
   # Configure
   git lfs install --skip-smudge
   
   # Cleanup
   rm git-lfs-linux-amd64-v3.7.0.tar.gz
   rm -r git-lfs-3.7.0/
   ```

2. **Configure Git/SSH (if needed)**
   ```bash
   # Generate SSH key
   ssh-keygen -t ed25519 -C "your_email@example.com"
   
   # Add to ssh-agent
   eval "$(ssh-agent -s)"
   ssh-add ~/.ssh/id_ed25519
   
   # Add public key to GitHub/GitLab
   cat ~/.ssh/id_ed25519.pub
   ```

3. **Install Git DRS**
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)"
   
   # Update PATH
   echo 'export PATH="$PATH:$HOME/.local/bin"' >> ~/.bash_profile
   source ~/.bash_profile
   ```

4. **Verify Installation**
   ```bash
   git-drs version
   ```

## Terra/Jupyter Installation (AnVIL Server)

**Target Environment**: Terra Jupyter notebooks targeting AnVIL DRS servers

### Steps

1. **Launch Jupyter Environment** in Terra

2. **Open Terminal** in Jupyter

3. **Install Dependencies**
   ```bash
   # Install Git DRS
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)"
   
   # Install DRS Downloader
   wget https://github.com/anvilproject/drs_downloader/releases/download/0.1.6-rc.4/drs_downloader
   chmod 755 drs_downloader
   ```

4. **Verify Installation**
   ```bash
   git-drs --help
   drs_downloader --help
   ```

5. **Example Workflow**
   ```bash
   # Clone example repository
   git clone https://github.com/quinnwai/super-cool-anvil-analysis.git
   cd super-cool-anvil-analysis/
   
   # Configure for your Terra project
   git drs init --server anvil --terraProject $GOOGLE_PROJECT
   
   # Work with manifests
   gsutil cp $WORKSPACE_BUCKET/anvil-manifest.tsv .
   git drs create-cache anvil-manifest.tsv
   
   # List and pull files
   git lfs ls-files
   git lfs pull -I data_tables_sequencing_dataset.tsv
   ```

## Local Installation (AnVIL Server)

**Target Environment**: Local development machine targeting AnVIL servers

### Steps

1. **Install Git DRS** (same as Gen3 local installation)
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/fix/install-error-macos/install.sh)"
   ```

2. **Get Terra Project ID**
   - Log in to [AnVIL Workspaces](https://anvil.terra.bio/#workspaces)
   - Select your workspace
   - Copy the Google Project ID from "CLOUD INFORMATION"

3. **Configure AnVIL Access**
   ```bash
   # Check existing configuration
   git drs list-config
   
   # If no AnVIL server configured, initialize it
   git drs init --server anvil --terraProject <your-terra-project-id>
   ```

## Build from Source

For development or custom builds:

```bash
# Clone repository
git clone https://github.com/calypr/git-drs.git
cd git-drs

# Build
go build

# Make accessible
export PATH=$PATH:$(pwd)
```

## Post-Installation

After installation, verify your setup:

```bash
# Check Git DRS version
git-drs version

# Check Git LFS
git lfs version

# View configuration
git drs list-config
```

## Next Steps

After installation, see:
- [Getting Started Guide](getting-started.md) for repository setup
- [Commands Reference](commands.md) for detailed usage
