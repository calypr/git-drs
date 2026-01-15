# Repository helper scripts

This repository includes small helper scripts used for listing and cleaning indexd records and bucket items.

## Scripts

- `create-minio-alias.sh` — Configure a MinIO\/S3 alias for `mc` using positional parameters: `alias`, `endpoint`, `access_key`, `secret_key`, optional `--insecure`.
- `list-indexd-sha256` — List indexd records with their did, sha256, file_name and resource.
- `list-s3-by-sha256` — Given a list of sha256 hashes, list S3 URLs for the files.
- `delete-s3-by-sha256` — Given a list of sha256 hashes, delete files from S3 by their sha256.

## Requirements

- `git` (repo must be clean or changes will be committed).
- `mc` (MinIO client) for `create-minio-alias.sh`.

## Credentials
- For `create-minio-alias.sh`, you need access credentials (access key and secret key) for the MinIO or S3-compatible service you are configuring.
- For `list-indexd-sha256`, ensure you have postgres pod and postgres user credentials.
-  Create a MinIO alias (positional parameters):
  ./create-minio-alias.sh myminio https://play.min.io YOURACCESS YOURSECRET --insecure
The `--insecure` flag is optional and will pass `--insecure` to `mc alias set` to skip TLS verification.


## Notes and safety

- Do not commit secrets. If you store credentials for tests, keep them out of the repository or add the file to `.gitignore`.
- List data first to be careful with destructive commands.
  - `list-indexd-sha256` will list the did, sha256, file_name and resource.  You can pipe the output of this script into:
    - list-s3-by-sha256 to get a list of S3 URLs for the files.
    - delete-s3-by-sha256 to delete files from S3 by their sha256.

## Usage
```bash
./list-indexd-sha256.sh <pod> <passwd> <resource> | ./delete-s3-by-sha256.sh <alias> <bucket>
./clean-indexd.sh <pod> <passwd> <resource>
```