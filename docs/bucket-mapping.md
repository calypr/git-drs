# Bucket Mapping

Bucket mapping controls where a given `organization/project` scope stores object bytes.

This is usually steward or admin setup, not normal end-user setup.

## What You Configure

There are three related layers:

1. Bucket credentials on the server
2. Organization-wide storage mapping
3. Project-specific storage mapping

The usual flow is:

```bash
git drs bucket add production \
  --bucket cbds \
  --region us-east-1 \
  --access-key "$AWS_ACCESS_KEY_ID" \
  --secret-key "$AWS_SECRET_ACCESS_KEY" \
  --s3-endpoint https://s3.amazonaws.com

git drs bucket add-organization production \
  --organization HTAN_INT \
  --path s3://cbds/htan-int

git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/bforepc
```

## 1. Add Bucket Credentials

Declare the storage credential on the target server:

```bash
git drs bucket add production \
  --bucket cbds \
  --region us-east-1 \
  --access-key "$AWS_ACCESS_KEY_ID" \
  --secret-key "$AWS_SECRET_ACCESS_KEY" \
  --s3-endpoint https://s3.amazonaws.com
```

Notes:

- this stores the bucket credential on the remote Syfon server
- `production` is an optional remote name; if omitted, `origin` is used
- `--s3-endpoint` is required by the current command surface

## 2. Add an Organization Mapping

Map an organization to a bucket path:

```bash
git drs bucket add-organization production \
  --organization HTAN_INT \
  --path s3://cbds/htan-int
```

This means:

- bucket: `cbds`
- base prefix for the organization: `htan-int`

Every project in that organization can inherit this mapping.

## 3. Add a Project Mapping

Map a project to a path:

```bash
git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/bforepc
```

This adds a project-specific mapping for `HTAN_INT/BForePC`.

## Path Format

Both `add-organization` and `add-project` require `--path` in storage URL form:

```bash
s3://bucket/prefix
gs://bucket/prefix
azblob://bucket/prefix
```

Important behavior:

- the bucket name is taken from the URL host
- the persisted prefix is the path portion after the bucket
- the bucket embedded in `--path` must match the resolved bucket

## How Resolution Works

When `git-drs` resolves the effective storage scope for an `organization/project`, it uses these rules:

### If an organization mapping exists

The organization mapping wins for the bucket.

Then:

- the organization prefix is used as the base
- if a project mapping also exists, its prefix is appended

Conceptually:

```text
effective_prefix = org_prefix + "/" + project_prefix
```

Example:

- organization mapping: `s3://cbds/htan-int`
- project mapping prefix: `bforepc`
- effective result: bucket `cbds`, prefix `htan-int/bforepc`

### If no organization mapping exists

An exact project mapping can be used directly.

Example:

- project mapping: `s3://cbds/htan-int/bforepc`
- effective result: bucket `cbds`, prefix `htan-int/bforepc`

### If neither mapping exists

Remote add and scoped upload setup will fail until a mapping is configured.

## Choosing a Layout

There are two common patterns.

### Hierarchical organization-first layout

```bash
git drs bucket add-organization production \
  --organization HTAN_INT \
  --path s3://cbds/htan-int

git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/bforepc
```

Result:

- organization root: `htan-int`
- project subpath: `bforepc`
- effective project path: `htan-int/bforepc`

Use this when all projects in an organization should live under one org root.

### Exact project-only layout

```bash
git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/htan-int/bforepc
```

Use this when you do not want a separate organization-level mapping.

## Overwriting Existing Mappings

If a mapping already exists, the command fails unless you pass `--force`.

Example:

```bash
git drs bucket add-project production \
  --organization HTAN_INT \
  --project BForePC \
  --path s3://cbds/bforepc-v2 \
  --force
```

## What End Users Usually Need To Know

Usually, end users do not need to know the real bucket name.

They only need:

- the target `organization/project`
- valid credentials
- a configured remote

Then they can run:

```bash
git drs remote add gen3 production HTAN_INT/BForePC --cred ~/.gen3/credentials.json
```

That remote setup resolves the bucket mapping from the server-side scope configuration.

## Related Commands

- [Getting Started](getting-started.md)
- [Commands Reference](commands.md)
- [Troubleshooting](troubleshooting.md)
