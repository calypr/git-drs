# Quick Start

This page is intentionally minimal. It gets a new user from zero to a working `git-drs` repository with as little explanation as possible.

If you want the workflow explained after setup, continue to [Getting Started](getting-started.md).

## 1. Install `git-drs`

This quick start uses release `v0.6.0`.

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

Verify:

```bash
git-drs version
```

## 2. Choose A Preset And Credential Source

See the non-secret server defaults included with your installed release:

```bash
git drs preset list
git drs preset show calypr
```

The built-in aliases are `calypr`, `terra`, `synapse`, and `cgc`. Only
Calypr/Gen3 and Terra currently have operational runtime adapters; Synapse and
CGC are catalog-only presets. Presets supply
the endpoint, provider adapter, and usual authentication method; they never
contain credentials.

For the Calypr/Gen3 workflow below, download your Gen3 API credentials JSON
from your commons profile page and save it somewhere stable, for example:

```bash
~/.gen3/credentials.json
```

Typical flow:

1. Sign in to the Gen3 portal.
2. Open your profile page.
3. Click `Create API Key`.
4. Download the JSON file.
5. Save it somewhere stable.

On the command line, refer to the file as `file:~/.gen3/credentials.json`.
Other supported credential sources are `env:VARIABLE`, `helper:NAME`,
`profile:NAME`, and `stdin`. Do not put a token or password directly in
`--credential`.

## 3. Connect An Existing Repository

```bash
git clone <repo-url>
cd <repo-name>
git drs remote add production calypr --scope <organization/project> \
  --credential file:~/.gen3/credentials.json
git drs pull
```

Use this path when the repository already contains tracked pointers and you want local file contents.

## 4. Start A New Repository

```bash
mkdir my-data-repo
cd my-data-repo
git init
git drs remote add production calypr --scope <organization/project> \
  --credential file:~/.gen3/credentials.json
git drs track "*.bam"
git add .gitattributes
git commit -m "Configure tracked files"
```

`production` is the local remote name and `calypr` is the preset. If the preset
name is also a suitable local name, the shorter form is
`git drs remote add calypr ...`.

## 5. Day-One Commands

Update Git history:

```bash
git pull
```

Hydrate tracked pointer files already present in the checkout:

```bash
git drs pull
```

Register/upload tracked objects and complete the managed push flow:

```bash
git drs push
```

Use plain `git push` only when you intentionally want Git-only ref updates without the managed `git-drs` upload/registration path.

## Read Next

- [Getting Started](getting-started.md) for the workflow model
- [Commands Reference](commands.md) for exact command behavior
