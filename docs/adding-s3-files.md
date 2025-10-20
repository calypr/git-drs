# Usage: git drs add-url

The `git drs add-url` command allows you to associate an S3 URL with a Git DRS repository without moving the actual data.

## Prerequisites
- AWS bucket credentials access 

## Command Syntax

```bash
git drs add-url s3://bucket/path/to/file --sha256 <sha256_hash> [--aws-access-key <key>] [--aws-secret-key <key>]
```

### Required Parameters
- `s3://bucket/path/to/file`: The S3 URL of the file to be added.
- `--sha256 <sha256_hash>`: The SHA256 hash of the file.

### Other Parameters
- `--aws-access-key <key>`: AWS access key for authentication
  - This flag takes precedence over the `AWS_ACCESS_KEY_ID` environment variable.
- `--aws-secret-key <key>`: AWS secret key for authentication.
  - The `--aws-secret-key` flag takes precedence over the `AWS_SECRET_ACCESS_KEY` environment variable.
-  `--endpoint`: Endpoint used to access bucket
  - Required for S3 buckets not registered in CALYPR 
-  `--region`: AWS regions
  - Required for S3 buckets not registered in CALYPR

## Examples

### Add a URL from a registered bucket
```bash
git lfs track "my-file"
git add .gitattributes

git drs add-url s3://my-bucket/path/to/my-file --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef --aws-access-key myAccessKey --aws-secret-key mySecretKey

git commit -m "add single file"
git push
```

### Add a URL from a unregistered bucket
```bash
export AWS_ACCESS_KEY_ID=myAccessKey
export AWS_SECRET_ACCESS_KEY=mySecretKey

git lfs track "my-file"
git add .gitattributes

git drs add-url s3://my-bucket/path/to/my-file --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef --endpoint https://bucket-endpoint.org --region us-west-2

git commit -m "add single file"
git push
```

### Register multiple files
```bash
git lfs track "directory/**"
git add .gitattributes

export AWS_ACCESS_KEY_ID=<your-key>
export AWS_SECRET_ACCESS_KEY=<your-secret>
git drs add-url s3://my-bucket/directory/my-file-1 --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef
git drs add-url s3://my-bucket/directory/subdir/my-file-2 --sha256 abcdef1234567890abcdef12345678901234567890abcdef1234567890abcdef

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
