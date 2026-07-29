# Publishing documentation with GitHub Pages

The [`Publish documentation`](../.github/workflows/pages.yaml) workflow builds
the end-user documentation selected in `mkdocs.yml` and renders the
[`anvil-terra-poc-presentation.md`](anvil-terra-poc-presentation.md) Marp deck.
It publishes both outputs as one GitHub Pages site. Do not edit or commit the
generated HTML. The Pages build explicitly selects Marp's `bespoke` template;
that template includes the browser runtime used for slide paging and keyboard
navigation, unlike the non-interactive `bare` template. It renders the deck to
a temporary build directory and copies the finished standalone HTML into the
site after MkDocs runs, preserving the embedded runtime and presentation styles.

## Enable GitHub Pages

An administrator only needs to configure Pages once for the repository:

1. Open the repository on GitHub and select **Settings**.
2. Select **Pages** under **Code and automation**.
3. Under **Build and deployment**, set **Source** to **GitHub Actions**.
4. If GitHub prompts you to enable Actions, open **Settings → Actions →
   General**, allow GitHub Actions to run, and save the setting.
5. Open **Actions → Publish documentation**, select **Run workflow**, choose the
   `main` branch, and select **Run workflow**.
6. When the workflow finishes, its `deploy` job shows the published site URL.
   The URL also appears in **Settings → Pages**.

The repository must permit the official actions used by the workflow. If the
organization restricts actions, allow these actions and their pinned major
versions:

- `actions/checkout@v4`
- `actions/configure-pages@v5`
- `actions/setup-python@v5`
- `actions/upload-pages-artifact@v3`
- `actions/deploy-pages@v4`

The workflow declares the required `pages: write` and `id-token: write`
permissions itself. No deploy key, personal access token, or `gh-pages` branch
is required.

## Publish updates

After Pages is enabled, push a change under `docs/`, `mkdocs.yml`, or the Pages
workflow to `main`. The workflow runs automatically, renders the selected
Markdown pages with MkDocs, renders the deck with the pinned Marp CLI version,
and deploys the combined site. A maintainer can also publish without a content
change by running the workflow manually from the **Actions** tab.

The generated site includes:

```text
/index.html
/quickstart/
/getting-started/
/commands/
/pointer-files/
/adding-s3-files/
/remove-files/
/troubleshooting/
/anvil-terra-poc-presentation.html
```

If the repository's default publishing branch is not `main`, update the
`on.push.branches` value in `.github/workflows/pages.yaml` before relying on
automatic deployments.

## Preview locally

With Python, Node.js, and npm installed, stage the selected end-user pages and
build the same combined site as CI:

```bash
python -m pip install mkdocs-material==9.6.14
rm -rf .pages-docs _site
mkdir .pages-docs
cp docs/{index,quickstart,getting-started,commands,pointer-files}.md .pages-docs/
cp docs/{adding-s3-files,remove-files,troubleshooting}.md .pages-docs/
cp docs/*.png .pages-docs/
npx --yes @marp-team/marp-cli@4.2.3 \
  docs/anvil-terra-poc-presentation.md \
  --html \
  --template bespoke \
  --output /tmp/anvil-terra-poc-presentation.html
mkdocs build --strict
cp /tmp/anvil-terra-poc-presentation.html \
  _site/anvil-terra-poc-presentation.html
python -m http.server --directory _site 8000
```

Open `http://localhost:8000/` in a browser. Keep dependency versions in these
commands synchronized with `.github/workflows/pages.yaml` when upgrading the
documentation toolchain.

## Troubleshooting

- **The workflow does not start after a push:** confirm the change is on
  `main` and touches `docs/**`, `mkdocs.yml`, or
  `.github/workflows/pages.yaml`.
- **The deploy job reports that Pages is not enabled:** repeat the enablement
  steps and confirm the Pages source is **GitHub Actions**, not **Deploy from a
  branch**.
- **An action is blocked:** ask an organization owner to allow the five
  official GitHub actions listed above.
- **The page is not immediately available:** wait for the `deploy` job to
  finish and use the URL shown in that job rather than guessing the repository
  URL.
