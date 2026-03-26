package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/lfs"
	"golang.org/x/oauth2/google"
)

// Download extracts the first non-empty access URL from a
// DRSObject, performs a HEAD preflight for that URL, downloads object bytes to
// a temporary file, and returns the computed SHA256 and temporary file path.
func Download(ctx context.Context, drsObj *drs.DRSObject) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if drsObj == nil {
		return "", "", fmt.Errorf("drs object is nil")
	}

	rawURL, err := firstAccessURL(drsObj)
	if err != nil {
		return "", "", err
	}

	headMeta, err := HeadObject(ctx, rawURL)
	if err != nil {
		return "", "", fmt.Errorf("head preflight failed: %w", err)
	}

	rc, err := openObjectReader(ctx, rawURL)
	if err != nil {
		return "", "", err
	}
	defer rc.Close()

	etag := headMeta.ETag
	subdir1, subdir2 := "xx", "yy"
	if len(etag) >= 4 {
		subdir1 = etag[0:2]
		subdir2 = etag[2:4]
	}
	objName := etag
	if objName == "" {
		objName = "unknown-etag"
	}
	tmpDir := filepath.Join(os.TempDir(), "git-drs", "tmp-objects", subdir1, subdir2)
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", tmpDir, err)
	}
	tmpPath := filepath.Join(tmpDir, objName)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", tmpPath, err)
	}

	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmpFile, h), rc); err != nil {
		return "", "", fmt.Errorf("download object: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return "", "", fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", "", fmt.Errorf("close temp file: %w", err)
	}

	computedSHA := fmt.Sprintf("%x", h.Sum(nil))

	_, lfsRoot, err := lfs.GetGitRootDirectories(ctx)
	if err != nil {
		return "", "", fmt.Errorf("get git root directories: %w", err)
	}

	oid := computedSHA // sha of sentinel drsObj.Checksums.SHA256
	dstDir := filepath.Join(lfsRoot, "objects", oid[0:2], oid[2:4])
	dstPath := filepath.Join(dstDir, oid)

	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dstDir, err)
	}

	if err := os.Rename(tmpPath, dstPath); err != nil {
		return "", "", fmt.Errorf("rename %s to %s: %w", tmpPath, dstPath, err)
	}
	cleanup = false
	return computedSHA, dstPath, nil

}

func firstAccessURL(drsObj *drs.DRSObject) (string, error) {
	if len(drsObj.AccessMethods) == 0 {
		return "", fmt.Errorf("drs object has no access methods")
	}
	for _, am := range drsObj.AccessMethods {
		u := strings.TrimSpace(am.AccessURL.URL)
		if u != "" {
			return u, nil
		}
	}
	return "", fmt.Errorf("drs object has no access URL")
}

func openObjectReader(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	switch DetectPlatform(rawURL) {
	case PlatformS3:
		params := S3ObjectParameters{
			S3URL:        rawURL,
			AWSAccessKey: os.Getenv(AWS_KEY_ENV_VAR),
			AWSSecretKey: os.Getenv(AWS_SECRET_ENV_VAR),
			AWSRegion:    os.Getenv(AWS_REGION_ENV_VAR),
			AWSEndpoint:  os.Getenv(AWS_ENDPOINT_URL_ENV_VAR),
		}
		return AgentFetchReader(ctx, params)
	case PlatformGCS:
		return openGCSReader(ctx, rawURL)
	case PlatformAzure:
		return openAzureReader(ctx, rawURL)
	default:
		return nil, fmt.Errorf("unsupported URL: cannot detect cloud platform for %q", rawURL)
	}
}

func openGCSReader(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	bucket, key, err := parseGCSURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("GCS: %w", err)
	}

	httpClient, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/devstorage.read_only")
	if err != nil {
		return nil, fmt.Errorf("GCS: build credentials (is %s set?): %w", GCSCredentialsEnvVar, err)
	}

	apiURL := fmt.Sprintf("%s/storage/v1/b/%s/o/%s?alt=media", gcsAPIBase,
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
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("GCS: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

func openAzureReader(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	account, container, blob, err := parseAzureURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("Azure: %w", err)
	}

	if v := os.Getenv(AzureAccountEnvVar); v != "" {
		account = v
	}
	storageKey := os.Getenv(AzureKeyEnvVar)
	sasToken := os.Getenv(AzureSASTokenEnvVar)
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

	base := azureBlobBase
	if base == "" {
		base = fmt.Sprintf("https://%s.blob.core.windows.net", account)
	}
	blobEndpoint := fmt.Sprintf("%s/%s/%s", base, container, blob)

	reqURL := blobEndpoint
	if sasToken != "" {
		reqURL = blobEndpoint + "?" + strings.TrimPrefix(sasToken, "?")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("Azure: build request: %w", err)
	}
	req.Header.Set("x-ms-date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("x-ms-version", azureBlobAPIVersion)

	if storageKey != "" && sasToken == "" {
		if err := signAzureSharedKey(req, account, container, blob, storageKey); err != nil {
			return nil, fmt.Errorf("Azure: sign request: %w", err)
		}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Azure: GET failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Azure: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Body, nil
}

// DownloadOLD deprecated: downloads the S3 object to a temporary file while computing its SHA256 hash.
// returns the computed SHA256 hash, temporary path and any error encountered.
func DownloadOLD(ctx context.Context, info *S3Object, s3Input S3ObjectParameters, lfsRoot string) (string, string, error) {
	// 2) object destination
	etag := info.ETag
	subdir1, subdir2 := "xx", "yy"
	if len(etag) >= 4 {
		subdir1 = etag[0:2]
		subdir2 = etag[2:4]
	}
	objName := etag
	if objName == "" {
		objName = "unknown-etag"
	}
	tmpDir := filepath.Join(lfsRoot, "tmp-objects", subdir1, subdir2)
	tmpObj := filepath.Join(tmpDir, objName)

	// 3) fetch bytes -> tmp, compute sha+count

	// Create the temporary directory and file where the S3 object will be streamed while computing its hash and size.
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", tmpDir, err)
	}

	f, err := os.Create(tmpObj)
	if err != nil {
		return "", "", fmt.Errorf("create %s: %w", tmpObj, err)
	}
	// ensure any leftover file is closed and error propagated via named return
	defer func() {
		if f != nil {
			if cerr := f.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close tmp file: %w", cerr)
			}
		}
	}()

	h := sha256.New()

	var reader io.ReadCloser
	reader, err = AgentFetchReader(ctx, s3Input)
	if err != nil {
		return "", "", fmt.Errorf("fetch reader: %w", err)
	}
	// ensure close on any early return; propagate close error via named return
	defer func() {
		if reader != nil {
			if cerr := reader.Close(); cerr != nil && err == nil {
				err = fmt.Errorf("close reader: %w", cerr)
			}
		}
	}()

	n, err := io.Copy(io.MultiWriter(f, h), reader)
	if err != nil {
		return "", "", fmt.Errorf("copy bytes to %s: %w", tmpObj, err)
	}

	// explicitly close reader and handle error
	if cerr := reader.Close(); cerr != nil {
		return "", "", fmt.Errorf("close reader: %w", cerr)
	}
	reader = nil

	// ensure data is flushed to disk
	if err = f.Sync(); err != nil {
		return "", "", fmt.Errorf("sync %s: %w", tmpObj, err)
	}

	// explicitly close tmp file before rename
	if cerr := f.Close(); cerr != nil {
		return "", "", fmt.Errorf("close %s: %w", tmpObj, cerr)
	}
	f = nil

	// use n (bytes written) to avoid unused var warnings
	_ = n

	// compute hex SHA256 of the fetched content
	computedSHA := fmt.Sprintf("%x", h.Sum(nil))
	return computedSHA, tmpObj, nil
}

// GetSHA256 computes the SHA256 hash of the input string and returns it as a hex-encoded string.
func GetSHA256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
