# Usage: git drs add-url

The `git drs add-url` command allows you to associate an S3 URL with a Git DRS repository without moving the actual data.

## Command Syntax

```bash
git drs add-url s3://bucket/path/to/file --sha256 <sha256_hash> [--aws-access-key <key>] [--aws-secret-key <key>]
```

### Required Parameters
- `s3://bucket/path/to/file`: The S3 URL of the file to be added.
- `--sha256 <sha256_hash>`: The SHA256 hash of the file.

### Other Parameters
- `--aws-access-key <key>`: AWS access key for authentication
  - Either pass it in as a flag or as a environment variable: 
- `--aws-secret-key <key>`: AWS secret key for authentication.
  - Either pass it in as a flag or as a environment variable: 

## Examples

### Single File
```bash
git lfs track "my-file"
git add .gitattributes

git drs add-url s3://my-bucket/my-file --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef --aws-access-key myAccessKey --aws-secret-key mySecretKey

git commit -m "add single file"
git push
```

### Multiple Files
```bash

git lfs track "directory/**"
git add .gitattributes

`export AWS_ACCESS_KEY_ID=<your-key>`
`export AWS_SECRET_ACCESS_KEY=<your-secret>`
git drs add-url s3://my-bucket/directory/my-file-1 --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef --aws-access-key myAccessKey 
git drs add-url s3://my-bucket/directory/subdir/my-file-2 --sha256 abcdef1234567890abcdef12345678901234567890abcdef1234567890abcdef --aws-access-key myAccessKey --aws-secret-key mySecretKey

git commit -m "add directory/ files"
git push
```

## Workflow
1. Validate the inputs:
   - Ensure the file is LFS tracked.
   - Validate the S3 URL format.
   - Validate the SHA256 hash length.
   - Validate AWS credentials.
2. Fetch metadata (file size and modified date) from the S3 object.
3. Create an indexd record on the server for the project using the provided hash, file size, and modified date.
4. Generate a Git LFS pointer file and add it to the Git index.

## Notes
- Ensure that the file is tracked by Git LFS before running this command.
- AWS credentials can be provided via environment variables or command-line flags.
- Use `git commit` and `git push` to finalize the changes.
