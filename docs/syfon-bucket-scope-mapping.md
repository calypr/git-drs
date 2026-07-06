# Creating a Syfon Bucket Scope Mapping

This fixes upload failures where Syfon can authenticate the request, but cannot
choose a storage bucket for the requested organization/project.

Example failure:

```text
failed batch register/upload workflow: upload error: GET https://calypr-dev.ohsu.edu/data/upload/... status 400 body=invalid input: no bucket scope configured for organization "cbds" project "monorepos"
```

That error means the server is missing a bucket scope mapping for
`cbds/monorepos`. It is usually steward/admin setup, not a normal end-user file
problem. `git-drs` can create the mapping if the caller has an admin token for
the target Syfon server.

## 1. Make Sure the Bucket Credential Exists

If the bucket credential is not already configured on Syfon, add it first:

```bash
git drs bucket add calypr-dev \
  --bucket cbds \
  --region us-east-1 \
  --access-key "$AWS_ACCESS_KEY_ID" \
  --secret-key "$AWS_SECRET_ACCESS_KEY" \
  --s3-endpoint https://s3.amazonaws.com
```

Use the real bucket name, region, endpoint, and credentials for the environment.
The remote name, `calypr-dev` above, must resolve to the Syfon endpoint and
token, or you can pass `--url`, `--token`, or `--cred`.

## 2. Add the Project Mapping

For the error above, create a project-specific mapping:

```bash
git drs bucket add-project calypr-dev \
  --organization cbds \
  --project monorepos \
  --path s3://cbds/monorepos
```

`--path` is the storage root Syfon should use for that scope. The bucket is read
from the URL host (`cbds`), and the remaining path (`monorepos`) is stored as
the prefix.

If a mapping already exists and needs to be replaced, add `--force`.

## 3. Retry the User Workflow

After the mapping exists, the user can retry:

```bash
git drs push
```

No change to the tracked file is required. The upload URL request will include
`organization=cbds&project=monorepos`, and Syfon should now resolve that scope to
the configured bucket path.

## When To Use an Organization Mapping

Use an organization mapping when every project in an organization should share a
common root:

```bash
git drs bucket add-organization calypr-dev \
  --organization cbds \
  --path s3://cbds
```

Then add project mappings only when a project needs a specific subpath. For most
one-off upload failures, `add-project` is the least surprising fix.

