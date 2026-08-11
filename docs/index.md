# Git DRS Documentation

`git-drs` brings Git-compatible versioning workflows to large data stored
behind Data Repository Service (DRS) providers. Use these end-user guides to
install the command, connect a repository, manage pointer-backed files, and
resolve common problems.

## Start here

- [Quick Start](quickstart.md) — install `git-drs`, configure a remote, and
  complete the first push and pull workflow.
- [Getting Started](getting-started.md) — understand the Git and DRS mental
  model and the most common repository workflows.
- [Commands Reference](commands.md) — look up the supported CLI commands and
  their options.

## Work with data

- [Pointer Files and Reference State](pointer-files.md) — learn what Git
  commits in place of large payloads.
- [Add Existing S3 Objects](adding-s3-files.md) — create references for data
  that is already in provider storage.
- [Globus Access Methods](globus.md) — log in with Globus Auth, configure a
  destination collection, and use Globus-backed hydration.
- [Access-Method Selection](access-method-selection-and-authentication.md) —
  choose automatic, preferred, or required transfer methods and understand
  fallback and readiness diagnostics.
- [Remove Files](remove-files.md) — remove tracked files and understand remote
  reconciliation behavior.
- [Troubleshooting](troubleshooting.md) — diagnose setup, authentication,
  tracking, push, and hydration problems.

## AnVIL/Terra proof of concept

View the [AnVIL/Terra DRS reference presentation](anvil-terra-poc-presentation.html)
for a visual overview of the reference-only workflow, architecture, security
boundary, and remaining production-validation work.

The presentation is generated from Markdown during the Pages build; its HTML
output is not stored in the repository.
