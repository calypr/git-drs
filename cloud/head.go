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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/oauth2/google"
)

// testable endpoint overrides – set only from tests.
var (
	gcsAPIBase    = "https://storage.googleapis.com"
	azureBlobBase = "" // when non-empty, replaces https://{account}.blob.core.windows.net
)

// ─────────────────────────────────────────────────────────────────────────────
// Public types
// ─────────────────────────────────────────────────────────────────────────────

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
	Platform     Platform // s3 | gcs | azure
	URL          string   // original URL as supplied
	Bucket       string   // bucket / container name
	Key          string   // object key / blob path
	SizeBytes    int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string // user-defined metadata, keys lower-cased
	EnvVarsUsed  []string          // env var names that were consumed
}

// ─────────────────────────────────────────────────────────────────────────────
// Entry point
// ─────────────────────────────────────────────────────────────────────────────

// HeadObject detects the cloud platform from rawURL, harvests the required
// credentials from the environment, performs a HEAD (or equivalent) operation,
// and returns normalised metadata.
func HeadObject(ctx context.Context, rawURL string) (*ObjectMeta, error) {
	switch DetectPlatform(rawURL) {
	case PlatformS3:
		return headS3(ctx, rawURL)
	case PlatformGCS:
		return headGCS(ctx, rawURL)
	case PlatformAzure:
		return headAzure(ctx, rawURL)
	default:
		return nil, fmt.Errorf("unsupported URL: cannot detect cloud platform for %q", rawURL)
	}
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

// ─────────────────────────────────────────────────────────────────────────────
// S3
// ─────────────────────────────────────────────────────────────────────────────

// s3EnvVars are the environment variables consumed for S3 access.
// Names are already declared as constants in s3.go in this package.
var s3EnvVars = []string{
	AWS_KEY_ENV_VAR,
	AWS_SECRET_ENV_VAR,
	AWS_REGION_ENV_VAR,
	AWS_ENDPOINT_URL_ENV_VAR,
}

func headS3(ctx context.Context, rawURL string) (*ObjectMeta, error) {
	accessKey := os.Getenv(AWS_KEY_ENV_VAR)
	secretKey := os.Getenv(AWS_SECRET_ENV_VAR)
	region := os.Getenv(AWS_REGION_ENV_VAR)
	endpoint := os.Getenv(AWS_ENDPOINT_URL_ENV_VAR)

	if accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("S3: %s and %s must be set", AWS_KEY_ENV_VAR, AWS_SECRET_ENV_VAR)
	}
	if region == "" {
		return nil, fmt.Errorf("S3: %s must be set", AWS_REGION_ENV_VAR)
	}

	bucket, key, err := parseS3URL(rawURL) // reuse existing helper in inspect.go
	if err != nil {
		return nil, fmt.Errorf("S3: %w", err)
	}

	params := S3ObjectParameters{
		S3URL:        rawURL,
		AWSAccessKey: accessKey,
		AWSSecretKey: secretKey,
		AWSRegion:    region,
		AWSEndpoint:  endpoint,
	}
	client, err := newS3Client(ctx, params) // reuse existing helper in inspect.go
	if err != nil {
		return nil, fmt.Errorf("S3: build client: %w", err)
	}

	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("S3: HeadObject %s/%s: %w", bucket, key, err)
	}

	meta := &ObjectMeta{
		Platform:    PlatformS3,
		URL:         rawURL,
		Bucket:      bucket,
		Key:         key,
		Metadata:    make(map[string]string),
		EnvVarsUsed: s3EnvVars,
	}
	if head.ContentLength != nil {
		meta.SizeBytes = *head.ContentLength
	}
	if head.ETag != nil {
		meta.ETag = strings.Trim(*head.ETag, `"`)
	}
	if head.ContentType != nil {
		meta.ContentType = *head.ContentType
	}
	if head.LastModified != nil {
		meta.LastModified = *head.LastModified
	}
	for k, v := range head.Metadata {
		meta.Metadata[strings.ToLower(k)] = v
	}
	return meta, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Google Cloud Storage
// ─────────────────────────────────────────────────────────────────────────────

const (
	GCSCredentialsEnvVar = "GOOGLE_APPLICATION_CREDENTIALS"
	GCSProjectEnvVar     = "GOOGLE_CLOUD_PROJECT"
)

var gcsEnvVars = []string{GCSCredentialsEnvVar, GCSProjectEnvVar}

// gcsObjectResponse is a minimal projection of the GCS JSON API object resource.
type gcsObjectResponse struct {
	Bucket      string            `json:"bucket"`
	Name        string            `json:"name"`
	Size        string            `json:"size"` // GCS returns size as a decimal string
	ETag        string            `json:"etag"`
	ContentType string            `json:"contentType"`
	Updated     time.Time         `json:"updated"`
	Metadata    map[string]string `json:"metadata"`
}

func headGCS(ctx context.Context, rawURL string) (*ObjectMeta, error) {
	bucket, key, err := parseGCSURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("GCS: %w", err)
	}

	// google.DefaultClient reads GOOGLE_APPLICATION_CREDENTIALS, Workload Identity,
	// or any other ADC source automatically.
	httpClient, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/devstorage.read_only")
	if err != nil {
		return nil, fmt.Errorf("GCS: build credentials (is %s set?): %w", GCSCredentialsEnvVar, err)
	}

	apiURL := fmt.Sprintf("%s/storage/v1/b/%s/o/%s", gcsAPIBase,
		url.PathEscape(bucket),
		url.PathEscape(key),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GCS: build request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GCS: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GCS: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var obj gcsObjectResponse
	if err := json.NewDecoder(resp.Body).Decode(&obj); err != nil {
		return nil, fmt.Errorf("GCS: decode response: %w", err)
	}

	meta := &ObjectMeta{
		Platform:     PlatformGCS,
		URL:          rawURL,
		Bucket:       bucket,
		Key:          key,
		ETag:         obj.ETag,
		ContentType:  obj.ContentType,
		LastModified: obj.Updated,
		Metadata:     make(map[string]string),
		EnvVarsUsed:  gcsEnvVars,
	}
	fmt.Sscanf(obj.Size, "%d", &meta.SizeBytes) // GCS size field is a decimal string
	for k, v := range obj.Metadata {
		meta.Metadata[strings.ToLower(k)] = v
	}
	return meta, nil
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

// ─────────────────────────────────────────────────────────────────────────────
// Azure Blob Storage
// ─────────────────────────────────────────────────────────────────────────────

const (
	AzureAccountEnvVar    = "AZURE_STORAGE_ACCOUNT"
	AzureKeyEnvVar        = "AZURE_STORAGE_KEY"
	AzureConnStringEnvVar = "AZURE_STORAGE_CONNECTION_STRING"
	AzureSASTokenEnvVar   = "AZURE_STORAGE_SAS_TOKEN"
	azureBlobAPIVersion   = "2020-08-04"
)

var azureEnvVars = []string{
	AzureAccountEnvVar,
	AzureKeyEnvVar,
	AzureConnStringEnvVar,
	AzureSASTokenEnvVar,
}

func headAzure(ctx context.Context, rawURL string) (*ObjectMeta, error) {
	account, container, blob, err := parseAzureURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("Azure: %w", err)
	}

	// Env vars override anything parsed from the URL.
	if v := os.Getenv(AzureAccountEnvVar); v != "" {
		account = v
	}
	storageKey := os.Getenv(AzureKeyEnvVar)
	sasToken := os.Getenv(AzureSASTokenEnvVar)

	// Parse connection string when explicit key/account are absent.
	if connStr := os.Getenv(AzureConnStringEnvVar); connStr != "" && (account == "" || storageKey == "") {
		account, storageKey = parseAzureConnString(connStr)
	}

	if account == "" {
		return nil, fmt.Errorf("Azure: %s must be set", AzureAccountEnvVar)
	}
	if storageKey == "" && sasToken == "" {
		return nil, fmt.Errorf("Azure: one of %s, %s, or %s must be set",
			AzureKeyEnvVar, AzureSASTokenEnvVar, AzureConnStringEnvVar)
	}

	// after:
	base := azureBlobBase
	if base == "" {
		base = fmt.Sprintf("https://%s.blob.core.windows.net", account)
	}
	blobEndpoint := fmt.Sprintf("%s/%s/%s", base, container, blob)

	reqURL := blobEndpoint
	if sasToken != "" {
		reqURL = blobEndpoint + "?" + strings.TrimPrefix(sasToken, "?")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("Azure: build request: %w", err)
	}

	now := time.Now().UTC().Format(http.TimeFormat)
	req.Header.Set("x-ms-date", now)
	req.Header.Set("x-ms-version", azureBlobAPIVersion)

	if storageKey != "" && sasToken == "" {
		if err := signAzureSharedKey(req, account, container, blob, storageKey); err != nil {
			return nil, fmt.Errorf("Azure: sign request: %w", err)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Azure: HEAD failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Azure: HTTP %d for %s/%s", resp.StatusCode, container, blob)
	}

	meta := &ObjectMeta{
		Platform:    PlatformAzure,
		URL:         rawURL,
		Bucket:      container,
		Key:         blob,
		ETag:        strings.Trim(resp.Header.Get("ETag"), `"`),
		ContentType: resp.Header.Get("Content-Type"),
		Metadata:    make(map[string]string),
		EnvVarsUsed: azureEnvVars,
	}
	if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		meta.LastModified = lm
	}
	fmt.Sscanf(resp.Header.Get("Content-Length"), "%d", &meta.SizeBytes)

	// User-defined metadata is returned as x-ms-meta-{key} response headers.
	for k, vals := range resp.Header {
		lower := strings.ToLower(k)
		if strings.HasPrefix(lower, "x-ms-meta-") && len(vals) > 0 {
			meta.Metadata[strings.TrimPrefix(lower, "x-ms-meta-")] = vals[0]
		}
	}
	return meta, nil
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

// signAzureSharedKey adds an Authorization: SharedKeyLite header.
// Implements the Shared Key Lite scheme for Blob service requests.
func signAzureSharedKey(req *http.Request, account, container, blob, storageKey string) error {
	keyBytes, err := base64.StdEncoding.DecodeString(storageKey)
	if err != nil {
		return fmt.Errorf("decode storage key (expected base64): %w", err)
	}

	date := req.Header.Get("x-ms-date")
	canonicalizedHeaders := fmt.Sprintf("x-ms-date:%s\nx-ms-version:%s", date, azureBlobAPIVersion)
	canonicalizedResource := fmt.Sprintf("/%s/%s/%s", account, container, blob)

	// Shared Key Lite string-to-sign for HEAD:
	// VERB\nContent-MD5\nContent-Type\nDate\nCanonicalizedHeaders\nCanonicalizedResource
	stringToSign := strings.Join([]string{
		"HEAD", // VERB
		"",     // Content-MD5  (empty for HEAD)
		"",     // Content-Type (empty for HEAD)
		"",     // Date         (superseded by x-ms-date)
		canonicalizedHeaders,
		canonicalizedResource,
	}, "\n")

	mac := hmac.New(sha256.New, keyBytes)
	mac.Write([]byte(stringToSign))
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req.Header.Set("Authorization", fmt.Sprintf("SharedKeyLite %s:%s", account, sig))
	return nil
}

// parseAzureConnString extracts AccountName and AccountKey from a connection
// string of the form:
//
//	DefaultEndpointsProtocol=https;AccountName=foo;AccountKey=bar==;...
func parseAzureConnString(connString string) (account, key string) {
	for _, part := range strings.Split(connString, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.TrimSpace(kv[0]) {
		case "AccountName":
			account = strings.TrimSpace(kv[1])
		case "AccountKey":
			key = strings.TrimSpace(kv[1])
		}
	}
	return
}
