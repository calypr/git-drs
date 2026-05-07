# Installation Guide

This guide covers installation of Git DRS across different environments and target DRS servers.

## Prerequisites

Git DRS requires Git to be installed. Install Git DRS using the steps below, then run `git drs install` to configure Git filters.

## Local Installation (Gen3 Server)

**Target Environment**: Local development machine targeting Gen3 data commons (e.g., CALYPR)

### Steps

1. **Install Git DRS**
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"
   ```

2. **Update PATH**
   ```bash
   # Add to your shell startup file (for example ~/.zshrc, ~/.bashrc, or ~/.profile)
   export PATH="$PATH:$HOME/.local/bin"
   source ~/.zshrc  # or source your shell startup file
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

1. **Configure Git/SSH (if needed)**
   ```bash
   # Generate SSH key
   ssh-keygen -t ed25519 -C "your_email@example.com"
   
   # Add to ssh-agent
   eval "$(ssh-agent -s)"
   ssh-add ~/.ssh/id_ed25519
   
   # Add public key to GitHub/GitLab
   cat ~/.ssh/id_ed25519.pub
   ```

2. **Install Git DRS**
   ```bash
   /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/calypr/git-drs/refs/heads/main/install.sh)"
   
   # Update PATH
   echo 'export PATH="$PATH:$HOME/.local/bin"' >> ~/.bash_profile
   source ~/.bash_profile
   ```

3. **Verify Installation**
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
