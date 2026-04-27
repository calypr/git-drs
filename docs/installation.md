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

4. **Install Global Git Filters for git-drs**
   ```bash
   git drs install
   ```

   This writes the `filter.drs` settings to your `~/.gitconfig`.

5. **Get Credentials**
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
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"
   
   # Update PATH
   echo 'export PATH="$PATH:$HOME/.local/bin"' >> ~/.bash_profile
   source ~/.bash_profile
   ```

4. **Verify Installation**
   ```bash
   git-drs version
   git drs install
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

# View configured remotes (after setup)
git drs remote list

# Verify git-drs global filter configuration
git config --global --get filter.drs.process
```

## Next Steps

After installation, see:

> **Navigation:** [Installation](installation.md) → [Getting Started](getting-started.md) → [Commands Reference](commands.md)

- **[Getting Started](getting-started.md)** - Repository setup and basic workflows
- **[Commands Reference](commands.md)** - Complete command documentation
