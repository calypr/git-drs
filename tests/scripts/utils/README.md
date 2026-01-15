# Repository helper scripts

This repository includes small helper scripts used for releasing and configuring services. The README below describes each script, requirements, and basic usage.

## Scripts

\- `bump-version.sh` — Update common version locations, commit, tag, and push a new release (general purpose).
\- `bump-go-version.sh` — Update version literals in tracked Go files, commit, tag, and push (Go-only).
\- `create-minio-alias.sh` — Configure a MinIO\/S3 alias for `mc` using positional parameters: `alias`, `endpoint`, `access_key`, `secret_key`, optional `--insecure`.

## Requirements

- `git` (repo must be clean or changes will be committed).
- `mc` (MinIO client) for `create-minio-alias.sh`.

## Credentials
- For `create-minio-alias.sh`, you need access credentials (access key and secret key) for the MinIO or S3-compatible service you are configuring.
- For `list-indexd-sha256`, ensure you have postgres pod and postgres user credentials.
-  Create a MinIO alias (positional parameters):
  ./create-minio-alias.sh myminio https://play.min.io YOURACCESS YOURSECRET --insecure
The `--insecure` flag is optional and will pass `--insecure` to `mc alias set` to skip TLS verification.

## Usage examples

Set a new release version (general):

    ./bump-version.sh v0.5.2

Set a new Go-only version:

    ./bump-go-version.sh v0.5.2


## Notes and safety

- Do not commit secrets. If you store credentials for tests, keep them out of the repository or add the file to `.gitignore`.
- List data first to be careful with destructive commands.
  - `list-indexd-sha256` will list the did, sha256, file_name and resource.  You can pipe the output of this script into:
    - list-s3-by-sha256 to get a list of S3 URLs for the files.
    - delete-s3-by-sha256 to delete files from S3 by their sha256.

