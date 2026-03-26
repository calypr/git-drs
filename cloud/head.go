// cloud/head.go
//
// Standalone utility that accepts any cloud storage URL, detects the provider,
// harvests credentials from the environment, performs a HEAD (or equivalent)
// operation, and returns normalised metadata.
//
// Supported providers and URL schemes:
//
//   S3    s3://bucket/key
//         https://bucket.s3.region.amazonaws.com/key
//
//   GCS   gs://bucket/key
//         https://storage.googleapis.com/bucket/key
//
//   Azure https://account.blob.core.windows.net/container/blob
//         az://container/blob   (account resolved from AZURE_STORAGE_ACCOUNT)

package cloud

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gocloud.dev/blob"
	_ "gocloud.dev/blob/azureblob"
	_ "gocloud.dev/blob/gcsblob"
	_ "gocloud.dev/blob/s3blob"
)

// openBucket is injectable for tests.
var openBucket = blob.OpenBucket

// Platform identifies the cloud storage provider.
type Platform string

const (
	PlatformS3      Platform = "s3"
	PlatformGCS     Platform = "gcs"
	PlatformAzure   Platform = "azure"
	PlatformUnknown Platform = "unknown"
)

// ObjectMeta is the normalised result of a cloud HEAD operation.
type ObjectMeta struct {
	Platform     Platform
	URL          string
	Bucket       string
	Key          string
	SizeBytes    int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
	EnvVarsUsed  []string
}

const (
	GCSCredentialsEnvVar  = "GOOGLE_APPLICATION_CREDENTIALS"
	GCSProjectEnvVar      = "GOOGLE_CLOUD_PROJECT"
	AzureAccountEnvVar    = "AZURE_STORAGE_ACCOUNT"
	AzureKeyEnvVar        = "AZURE_STORAGE_KEY"
	AzureConnStringEnvVar = "AZURE_STORAGE_CONNECTION_STRING"
	AzureSASTokenEnvVar   = "AZURE_STORAGE_SAS_TOKEN"
)

var s3EnvVars = []string{
	AWS_KEY_ENV_VAR,
	AWS_SECRET_ENV_VAR,
	AWS_REGION_ENV_VAR,
	AWS_ENDPOINT_URL_ENV_VAR,
}

var gcsEnvVars = []string{GCSCredentialsEnvVar, GCSProjectEnvVar}

var azureEnvVars = []string{
	AzureAccountEnvVar,
	AzureKeyEnvVar,
	AzureConnStringEnvVar,
	AzureSASTokenEnvVar,
}

// HeadObject detects the cloud platform from rawURL, opens the corresponding
// gocloud.dev/blob bucket, and returns normalised metadata.
func HeadObject(ctx context.Context, rawURL string) (*ObjectMeta, error) {
	platform := DetectPlatform(rawURL)
	if platform == PlatformUnknown {
		return nil, fmt.Errorf("unsupported URL: cannot detect cloud platform for %q", rawURL)
	}

	// Preserve the previous S3 validation behaviour and clearer error messages.
	if platform == PlatformS3 {
		if os.Getenv(AWS_KEY_ENV_VAR) == "" || os.Getenv(AWS_SECRET_ENV_VAR) == "" {
			return nil, fmt.Errorf("S3: %s and %s must be set", AWS_KEY_ENV_VAR, AWS_SECRET_ENV_VAR)
		}
		if os.Getenv(AWS_REGION_ENV_VAR) == "" {
			return nil, fmt.Errorf("S3: %s must be set", AWS_REGION_ENV_VAR)
		}
	}

	bucketURL, bucketName, key, err := toCDKBucketURL(rawURL, platform)
	if err != nil {
		return nil, err
	}

	b, err := openBucket(ctx, bucketURL)
	if err != nil {
		return nil, fmt.Errorf("%s: open bucket: %w", platform, err)
	}
	defer func() { _ = b.Close() }()

	attrs, err := b.Attributes(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%s: attributes %s: %w", platform, key, err)
	}

	meta := &ObjectMeta{
		Platform:     platform,
		URL:          rawURL,
		Bucket:       bucketName,
		Key:          key,
		SizeBytes:    attrs.Size,
		ETag:         strings.Trim(attrs.ETag, `"`),
		ContentType:  attrs.ContentType,
		LastModified: attrs.ModTime,
		Metadata:     make(map[string]string, len(attrs.Metadata)),
		EnvVarsUsed:  envVarsForPlatform(platform),
	}
	for k, v := range attrs.Metadata {
		meta.Metadata[k] = v
	}
	return meta, nil
}

// DetectPlatform returns the Platform for rawURL based on its scheme and host.
func DetectPlatform(rawURL string) Platform {
	u, err := url.Parse(rawURL)
	if err != nil {
		return PlatformUnknown
	}
	switch strings.ToLower(u.Scheme) {
	case "s3":
		return PlatformS3
	case "gs":
		return PlatformGCS
	case "az", "azblob":
		return PlatformAzure
	case "http", "https":
		host := strings.ToLower(u.Hostname())
		switch {
		case strings.Contains(host, ".blob.core.windows.net"):
			return PlatformAzure
		case strings.HasSuffix(host, "storage.googleapis.com"):
			return PlatformGCS
		case strings.Contains(host, ".s3.") ||
			strings.Contains(host, ".s3-") ||
			strings.HasPrefix(host, "s3."):
			return PlatformS3
		}
	}
	return PlatformUnknown
}

func toCDKBucketURL(rawURL string, platform Platform) (bucketURL, bucketName, key string, err error) {
	switch platform {
	case PlatformS3:
		bucketName, key, err = parseS3URL(rawURL)
		if err != nil {
			return "", "", "", fmt.Errorf("S3: %w", err)
		}
		params := url.Values{}
		if region := os.Getenv(AWS_REGION_ENV_VAR); region != "" {
			params.Set("region", region)
		}
		if endpoint := os.Getenv(AWS_ENDPOINT_URL_ENV_VAR); endpoint != "" {
			params.Set("endpoint", endpoint)
			params.Set("hostname_immutable", "true")
			params.Set("use_path_style", "true")
		}
		bucketURL = "s3://" + bucketName
		if len(params) > 0 {
			bucketURL += "?" + params.Encode()
		}
		return bucketURL, bucketName, key, nil
	case PlatformGCS:
		bucketName, key, err = parseGCSURL(rawURL)
		if err != nil {
			return "", "", "", fmt.Errorf("GCS: %w", err)
		}
		return "gs://" + bucketName, bucketName, key, nil
	case PlatformAzure:
		_, bucketName, key, err = parseAzureURL(rawURL)
		if err != nil {
			return "", "", "", fmt.Errorf("Azure: %w", err)
		}
		return "azblob://" + bucketName, bucketName, key, nil
	default:
		return "", "", "", fmt.Errorf("unsupported platform %q", platform)
	}
}

func envVarsForPlatform(platform Platform) []string {
	switch platform {
	case PlatformS3:
		return s3EnvVars
	case PlatformGCS:
		return gcsEnvVars
	case PlatformAzure:
		return azureEnvVars
	default:
		return nil
	}
}

// parseGCSURL handles gs://bucket/key and https://storage.googleapis.com/bucket/key.
func parseGCSURL(rawURL string) (bucket, key string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", err
	}
	switch u.Scheme {
	case "gs":
		return u.Host, strings.TrimPrefix(u.Path, "/"), nil
	case "http", "https":
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			return "", "", fmt.Errorf("cannot parse bucket/key from GCS URL: %s", rawURL)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", fmt.Errorf("unsupported GCS scheme %q in %s", u.Scheme, rawURL)
	}
}

// parseAzureURL handles:
//
//	https://account.blob.core.windows.net/container/blob
//	az://container/blob   (account resolved from AZURE_STORAGE_ACCOUNT)
func parseAzureURL(rawURL string) (account, container, blob string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", err
	}
	switch u.Scheme {
	case "az", "azblob":
		container = u.Host
		blob = strings.TrimPrefix(u.Path, "/")
	case "http", "https":
		host := u.Hostname()
		if idx := strings.Index(host, ".blob.core.windows.net"); idx != -1 {
			account = host[:idx]
		}
		parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			return "", "", "", fmt.Errorf("cannot parse container/blob from Azure URL: %s", rawURL)
		}
		container, blob = parts[0], parts[1]
	default:
		return "", "", "", fmt.Errorf("unsupported Azure URL scheme %q in %s", u.Scheme, rawURL)
	}
	if container == "" || blob == "" {
		return "", "", "", fmt.Errorf("Azure URL missing container or blob: %s", rawURL)
	}
	return account, container, blob, nil
}
