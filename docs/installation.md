# Installation Guide

This page is only about getting the `git-drs` binary onto a machine. It does not teach the repository workflow.

If you want the shortest onboarding path, use [Quick Start](quickstart.md). If you want the workflow explained, use [Getting Started](getting-started.md).

## Release Install

This guide uses release `v0.6.0`.

- [GitHub Releases](https://github.com/calypr/git-drs/releases)

=== "macOS (Apple Silicon)"

    ```bash
    curl -L -o git-drs-darwin-arm64-v0.6.0.tar.gz \
      https://github.com/calypr/git-drs/releases/download/v0.6.0/git-drs-darwin-arm64-v0.6.0.tar.gz
    tar -xzf git-drs-darwin-arm64-v0.6.0.tar.gz
    install -m 0755 git-drs "$HOME/.local/bin/git-drs"
    ```

=== "macOS (Intel)"

    ```bash
    curl -L -o git-drs-darwin-amd64-v0.6.0.tar.gz \
      https://github.com/calypr/git-drs/releases/download/v0.6.0/git-drs-darwin-amd64-v0.6.0.tar.gz
    tar -xzf git-drs-darwin-amd64-v0.6.0.tar.gz
    install -m 0755 git-drs "$HOME/.local/bin/git-drs"
    ```

=== "Linux (x86_64)"

    ```bash
    curl -L -o git-drs-linux-amd64-v0.6.0.tar.gz \
      https://github.com/calypr/git-drs/releases/download/v0.6.0/git-drs-linux-amd64-v0.6.0.tar.gz
    tar -xzf git-drs-linux-amd64-v0.6.0.tar.gz
    install -m 0755 git-drs "$HOME/.local/bin/git-drs"
    ```

=== "Linux (arm64)"

    ```bash
    curl -L -o git-drs-linux-arm64-v0.6.0.tar.gz \
      https://github.com/calypr/git-drs/releases/download/v0.6.0/git-drs-linux-arm64-v0.6.0.tar.gz
    tar -xzf git-drs-linux-arm64-v0.6.0.tar.gz
    install -m 0755 git-drs "$HOME/.local/bin/git-drs"
    ```

=== "Windows"

    There is no packaged Windows release in this flow. Build `git-drs` from source instead.

## PATH

If `$HOME/.local/bin` is not already in your shell path, add it in your shell startup file.

Example:

```bash
export PATH="$PATH:$HOME/.local/bin"
```

## Verify The Install

```bash
git-drs version
git drs version
```

## Build From Source

Use this when you need a local development build or a platform without a packaged release.

```bash
git clone https://github.com/calypr/git-drs.git
cd git-drs
go build
```

Then move or copy the built binary somewhere on your `PATH`.

## What Installation Does Not Do

Installing the binary does not configure a repository. Repository-local setup now happens when you run:

```bash
git drs remote add ...
```

## Read Next

- [Quick Start](quickstart.md) for first setup on a real repo
- [Getting Started](getting-started.md) for the workflow model
