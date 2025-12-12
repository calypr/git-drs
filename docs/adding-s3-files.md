# Adding S3 Files to Git DRS

The `git drs add-url` command allows you to associate an S3 URL with a Git DRS repository without moving the actual data. This command registers the S3 file location in the Gen3 indexd service and creates a Git LFS pointer file.

## Use Cases

There are two main use cases for adding S3 files:

### 1. Adding S3 Files from Gen3-Registered Buckets
If the S3 bucket is already registered in Gen3, the system can automatically retrieve the region and endpoint information from the Gen3 configuration. You only need to supply AWS credentials.

### 2. Adding S3 Files from Non-Registered Buckets
If the S3 bucket is not registered in Gen3, you must provide both AWS credentials and bucket configuration (region and endpoint URL).

## AWS Configuration

This command follows the standard AWS CLI authentication and configuration precedence as documented in the [AWS CLI Authentication Guide](https://docs.aws.amazon.com/cli/v1/userguide/cli-chap-authentication.html)

### Configuration Priority (Highest to Lowest)

1. **Command-line flags**: `--aws-access-key-id`, `--aws-secret-access-key`, `--region`, `--endpoint-url`
2. **Environment variables**: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`, `AWS_ENDPOINT_URL`
3. **AWS configuration files**: `~/.aws/credentials` first, then `~/.aws/config`
4. **Gen3 bucket registration**: For registered buckets, region and endpoint are retrieved from Gen3
5. **IAM roles**: For EC2 instances or containers with attached IAM roles

See the [AWS CLI Configuration Guide](https://github.com/aws/aws-cli#configuration) for the various ways to set up your credentials.

## Prerequisites

- Git LFS tracking must be configured for the file
- AWS credentials with read access to the S3 bucket
- For non-registered buckets: AWS region and endpoint URL

## Command Syntax

```bash
git drs add-url s3://bucket/path/to/file --sha256 <sha256_hash> [options]
```

### Required Parameters

- `s3://bucket/path/to/file`: The S3 URL of the file to be added
- `--sha256 <sha256_hash>`: The SHA256 hash of the file (64-character hexadecimal string)

### Optional Parameters

- `--aws-access-key-id <key>`: AWS access key for authentication
- `--aws-secret-access-key <key>`: AWS secret key for authentication
- `--region <region>`: AWS region (e.g., `us-west-2`, `us-east-1`)
  - Required for buckets not registered in Gen3 (unless configured in AWS config file)
- `--endpoint-url <url>`: S3 endpoint URL (e.g., `https://s3.example.com`)
  - Required for buckets not registered in Gen3 (unless configured in AWS config file)

## Examples

### Example 1: Gen3-Registered Bucket with Command-Line Credentials

If your bucket is registered in Gen3, you only need to provide AWS credentials:

```bash
# Track the file with Git LFS
git lfs track "my-file"
git add .gitattributes

# Add the S3 file using command-line credentials
git drs add-url s3://my-registered-bucket/path/to/my-file \
  --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef \
  --aws-access-key-id myAccessKey \
  --aws-secret-access-key mySecretKey

# Commit and push
git commit -m "Add file from registered bucket"
git push
```

### Example 2: Gen3-Registered Bucket with Environment Variables

```bash
# Set AWS credentials via environment variables
export AWS_ACCESS_KEY_ID=myAccessKey
export AWS_SECRET_ACCESS_KEY=mySecretKey

# Track the file with Git LFS
git lfs track "my-file"
git add .gitattributes

# Add the S3 file (credentials from environment)
git drs add-url s3://my-registered-bucket/path/to/my-file \
  --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef

# Commit and push
git commit -m "Add file from registered bucket"
git push
```

### Example 3: Non-Registered Bucket with Command-Line Credentials

For buckets not registered in Gen3, provide region and endpoint:

```bash
# Set credentials via environment variables
export AWS_ACCESS_KEY_ID=myAccessKey
export AWS_SECRET_ACCESS_KEY=mySecretKey

# Track the file with Git LFS
git lfs track "my-file"
git add .gitattributes

# Add the S3 file with region and endpoint
git drs add-url s3://my-custom-bucket/path/to/my-file \
  --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef \
  --region us-west-2 \
  --endpoint-url https://s3.custom-provider.com

# Commit and push
git commit -m "Add file from custom bucket"
git push
```

### Example 4: Non-Registered Bucket with AWS Configuration Files

You can also configure AWS credentials and settings in `~/.aws/credentials` and `~/.aws/config`:

**~/.aws/credentials:**
```ini
[default]
aws_access_key_id = myAccessKey
aws_secret_access_key = mySecretKey
```

**~/.aws/config:**
```ini
[default]
region = us-west-2
s3 =
  endpoint_url = https://s3.custom-provider.com
```

Then run the command without any credential flags:

```bash
git lfs track "my-file"
git add .gitattributes

# Credentials and configuration loaded from ~/.aws/ files
git drs add-url s3://my-bucket/path/to/my-file \
  --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef

git commit -m "Add file using AWS config files"
git push
```

### Example 5: Multiple Files from Registered Bucket

```bash
# Track all files in a directory
git lfs track "data-directory/**"
git add .gitattributes

# Set credentials once
export AWS_ACCESS_KEY_ID=myAccessKey
export AWS_SECRET_ACCESS_KEY=mySecretKey

# Add multiple files
git drs add-url s3://my-bucket/data-directory/file-1.dat \
  --sha256 1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef

git drs add-url s3://my-bucket/data-directory/subdir/file-2.dat \
  --sha256 abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890

git drs add-url s3://my-bucket/data-directory/file-3.dat \
  --sha256 fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321

# Commit all at once
git commit -m "Add multiple data files"
git push
```


## Notes

- **Git LFS Tracking**: Files must be tracked by Git LFS before running `add-url`. Use `git lfs track <pattern>` to configure tracking.
- **SHA256 Hash**: You must calculate the SHA256 hash of your file beforehand. Use `shasum -a 256 <file>` or similar tools.
- **Credentials Security**: Avoid putting credentials directly in command-line history. Use environment variables or AWS configuration files.
- **Bucket Registration**: For frequently used buckets, consider registering them in Gen3 to simplify the process.
- **Multiple URLs**: If a file is already registered, running `add-url` with a different S3 URL will add that URL to the existing record.
- **Project Isolation**: Each Git DRS project maintains separate indexd records, even for identical file hashes.

## Troubleshooting

### "file is not tracked by LFS"
Run `git lfs track <pattern>` to track the file pattern, then `git add .gitattributes`.

### "Unable to get bucket details"
This means the bucket is not registered in Gen3. Provide `--region` and `--endpoint-url` flags or configure them in your AWS config file.

### "unable to load AWS SDK config"
Check your AWS configuration:
- Verify credentials are set (via flags, environment, or `~/.aws/credentials`)
- Ensure `~/.aws/config` file is valid if you're using it
- Check that IAM roles are properly configured if running on EC2/ECS

### "failed to head object"
This usually means:
- Credentials don't have permission to access the object
- The S3 URL is incorrect
- The endpoint or region is misconfigured
- Network connectivity issues
