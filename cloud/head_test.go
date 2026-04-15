package cloud

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/calypr/data-client/drs"
	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
)

func injectBucket(t *testing.T, key string, content []byte, contentType string, metadata map[string]string) {
	t.Helper()

	old := openBucket
	t.Cleanup(func() { openBucket = old })

	openBucket = func(ctx context.Context, _ string) (*blob.Bucket, error) {
		b := memblob.OpenBucket(nil)
		opts := &blob.WriterOptions{ContentType: contentType}
		if metadata != nil {
			opts.Metadata = make(map[string]string, len(metadata))
			for k, v := range metadata {
				opts.Metadata[k] = v
			}
		}
		if err := b.WriteAll(ctx, key, content, opts); err != nil {
			_ = b.Close()
			return nil, fmt.Errorf("seed memblob object: %w", err)
		}
		return b, nil
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// DetectPlatform
// ─────────────────────────────────────────────────────────────────────────────

func TestDetectPlatform(t *testing.T) {
	tests := []struct {
		url  string
		want Platform
	}{
		// S3 – native scheme
		{"s3://my-bucket/key.txt", PlatformS3},
		{"s3://my-bucket/deep/path/key.txt", PlatformS3},
		// S3 – virtual-hosted HTTPS
		{"https://my-bucket.s3.amazonaws.com/key", PlatformS3},
		{"https://my-bucket.s3.us-east-1.amazonaws.com/key", PlatformS3},
		{"https://my-bucket.s3-us-west-2.amazonaws.com/key", PlatformS3},
		// S3 – path-style HTTPS
		{"https://s3.amazonaws.com/bucket/key", PlatformS3},
		// GCS – native scheme
		{"gs://my-bucket/key.txt", PlatformGCS},
		{"gs://my-bucket/deep/path/key.txt", PlatformGCS},
		// GCS – HTTPS
		{"https://storage.googleapis.com/bucket/key", PlatformGCS},
		// Azure – native schemes
		{"az://container/blob.txt", PlatformAzure},
		{"azblob://container/blob.txt", PlatformAzure},
		// Azure – HTTPS
		{"https://myaccount.blob.core.windows.net/container/blob", PlatformAzure},
		// Unknown
		{"http://example.com/file", PlatformUnknown},
		{"ftp://server/file", PlatformUnknown},
		{"not-a-url", PlatformUnknown},
		{"", PlatformUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := DetectPlatform(tt.url); got != tt.want {
				t.Errorf("DetectPlatform(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseGCSURL
// ─────────────────────────────────────────────────────────────────────────────

func TestParseGCSURL(t *testing.T) {
	tests := []struct {
		rawURL  string
		bucket  string
		key     string
		wantErr bool
	}{
		{"gs://my-bucket/path/to/file.txt", "my-bucket", "path/to/file.txt", false},
		{"gs://my-bucket/file.txt", "my-bucket", "file.txt", false},
		{"https://storage.googleapis.com/my-bucket/path/to/file.txt", "my-bucket", "path/to/file.txt", false},
		{"https://storage.googleapis.com/b/deep/key.txt", "b", "deep/key.txt", false},
		// errors
		{"https://storage.googleapis.com/bucket-only", "", "", true},
		{"ftp://bucket/key", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			bucket, key, err := parseGCSURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseGCSURL(%q) err=%v, wantErr=%v", tt.rawURL, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if bucket != tt.bucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.bucket)
			}
			if key != tt.key {
				t.Errorf("key = %q, want %q", key, tt.key)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseAzureURL
// ─────────────────────────────────────────────────────────────────────────────

func TestParseAzureURL(t *testing.T) {
	tests := []struct {
		rawURL    string
		account   string
		container string
		blob      string
		wantErr   bool
	}{
		{
			"https://myaccount.blob.core.windows.net/mycontainer/path/to/blob.txt",
			"myaccount", "mycontainer", "path/to/blob.txt", false,
		},
		{
			"az://mycontainer/path/to/blob.txt",
			"", "mycontainer", "path/to/blob.txt", false,
		},
		{
			"azblob://mycontainer/blob.txt",
			"", "mycontainer", "blob.txt", false,
		},
		// errors
		{"https://myaccount.blob.core.windows.net/container-only", "", "", "", true},
		{"az://container", "", "", "", true},
		{"ftp://container/blob", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.rawURL, func(t *testing.T) {
			account, container, blobKey, err := parseAzureURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAzureURL(%q) err=%v, wantErr=%v", tt.rawURL, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if account != tt.account {
				t.Errorf("account = %q, want %q", account, tt.account)
			}
			if container != tt.container {
				t.Errorf("container = %q, want %q", container, tt.container)
			}
			if blobKey != tt.blob {
				t.Errorf("blob = %q, want %q", blobKey, tt.blob)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// signAzureSharedKey
// ─────────────────────────────────────────────────────────────────────────────

func TestSignAzureSharedKey(t *testing.T) {
	storageKey := base64.StdEncoding.EncodeToString([]byte("test-storage-key"))

	newReq := func() *http.Request {
		req, err := http.NewRequest(http.MethodHead, "https://account.blob.core.windows.net/container/blob.bin", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("x-ms-date", "Mon, 24 Mar 2026 12:00:00 GMT")
		req.Header.Set("x-ms-version", azureBlobAPIVersion)
		return req
	}

	req1 := newReq()
	if err := signAzureSharedKey(req1, "account", "container", "blob.bin", storageKey); err != nil {
		t.Fatalf("signAzureSharedKey error: %v", err)
	}
	auth := req1.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "SharedKeyLite account:") {
		t.Errorf("Authorization = %q, want prefix %q", auth, "SharedKeyLite account:")
	}

	// Same inputs must produce the same signature (deterministic HMAC).
	req2 := newReq()
	_ = signAzureSharedKey(req2, "account", "container", "blob.bin", storageKey)
	if req1.Header.Get("Authorization") != req2.Header.Get("Authorization") {
		t.Error("signAzureSharedKey is not deterministic")
	}

	// Different key must produce a different signature.
	req3 := newReq()
	otherKey := base64.StdEncoding.EncodeToString([]byte("other-storage-key"))
	_ = signAzureSharedKey(req3, "account", "container", "blob.bin", otherKey)
	if req1.Header.Get("Authorization") == req3.Header.Get("Authorization") {
		t.Error("different key must produce different signature")
	}

	// Invalid base64 key must return an error.
	req4 := newReq()
	if err := signAzureSharedKey(req4, "account", "container", "blob.bin", "not-base64!!!"); err == nil {
		t.Error("expected error for invalid base64 key")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// S3
// ─────────────────────────────────────────────────────────────────────────────

func TestHeadS3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("Content-Length", "42")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"abc123def456"`)
		w.Header().Set("Last-Modified", "Mon, 24 Mar 2026 12:00:00 GMT")
		w.Header().Set("x-amz-meta-sha256", "deadbeef")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv(AWS_KEY_ENV_VAR, "fake-access-key")
	t.Setenv(AWS_SECRET_ENV_VAR, "fake-secret-key")
	t.Setenv(AWS_REGION_ENV_VAR, "us-east-1")
	t.Setenv(AWS_ENDPOINT_URL_ENV_VAR, srv.URL)

	meta, err := HeadObject(context.Background(), "s3://test-bucket/path/to/file.bin")
	if err != nil {
		t.Fatalf("HeadObject error: %v", err)
	}

	if meta.Platform != PlatformS3 {
		t.Errorf("Platform = %q, want %q", meta.Platform, PlatformS3)
	}
	if meta.Bucket != "test-bucket" {
		t.Errorf("Bucket = %q, want test-bucket", meta.Bucket)
	}
	if meta.Key != "path/to/file.bin" {
		t.Errorf("Key = %q, want path/to/file.bin", meta.Key)
	}
	if meta.SizeBytes != 42 {
		t.Errorf("SizeBytes = %d, want 42", meta.SizeBytes)
	}
	if meta.ETag != "abc123def456" {
		t.Errorf("ETag = %q, want abc123def456", meta.ETag)
	}
	if meta.ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q, want application/octet-stream", meta.ContentType)
	}
	if got := meta.Metadata["sha256"]; got != "deadbeef" {
		t.Errorf("Metadata[sha256] = %q, want deadbeef", got)
	}
	if len(meta.EnvVarsUsed) == 0 {
		t.Error("EnvVarsUsed should not be empty")
	}
}

func TestHeadS3_MissingCredentials(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		secret  string
		region  string
		wantMsg string
	}{
		{"no key or secret", "", "", "us-east-1", AWS_KEY_ENV_VAR},
		{"no region", "k", "s", "", AWS_REGION_ENV_VAR},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(AWS_KEY_ENV_VAR, tt.key)
			t.Setenv(AWS_SECRET_ENV_VAR, tt.secret)
			t.Setenv(AWS_REGION_ENV_VAR, tt.region)

			_, err := HeadObject(context.Background(), "s3://bucket/key.txt")
			if err == nil {
				t.Fatal("expected error when credentials are missing")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error %q should mention %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GCS
// ─────────────────────────────────────────────────────────────────────────────

// buildGCSServiceAccountJSON returns a service_account credential JSON with
// token_uri pointing at tokenURL, signed by the provided RSA private key PEM.
// The google auth library uses token_uri from the JSON for the JWT bearer flow.
func buildGCSServiceAccountJSON(t *testing.T, tokenURL, privKeyPEM string) []byte {
	t.Helper()
	sa := map[string]string{
		"type":                        "service_account",
		"project_id":                  "test-project",
		"private_key_id":              "test-key-id",
		"private_key":                 privKeyPEM,
		"client_email":                "test@test-project.iam.gserviceaccount.com",
		"client_id":                   "123456789",
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"token_uri":                   tokenURL,
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
	}
	data, err := json.Marshal(sa)
	if err != nil {
		t.Fatalf("marshal service account JSON: %v", err)
	}
	return data
}

func TestHeadGCS(t *testing.T) {
	const content = "gcs object content"
	injectBucket(t, "path/to/data.bin", []byte(content), "application/octet-stream", map[string]string{
		"project": "calypr",
		"sha256": "cafebabe",
	})

	meta, err := HeadObject(context.Background(), "gs://gcs-bucket/path/to/data.bin")
	if err != nil {
		t.Fatalf("HeadObject error: %v", err)
	}

	if meta.Platform != PlatformGCS {
		t.Errorf("Platform = %q, want %q", meta.Platform, PlatformGCS)
	}
	if meta.Bucket != "gcs-bucket" {
		t.Errorf("Bucket = %q, want gcs-bucket", meta.Bucket)
	}
	if meta.Key != "path/to/data.bin" {
		t.Errorf("Key = %q, want path/to/data.bin", meta.Key)
	}
	if meta.SizeBytes != int64(len(content)) {
		t.Errorf("SizeBytes = %d, want %d", meta.SizeBytes, len(content))
	}
	if meta.ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q, want application/octet-stream", meta.ContentType)
	}
	if got := meta.Metadata["project"]; got != "calypr" {
		t.Errorf("Metadata[project] = %q, want calypr", got)
	}
	if got := meta.Metadata["sha256"]; got != "cafebabe" {
		t.Errorf("Metadata[sha256] = %q, want cafebabe", got)
	}
	if len(meta.EnvVarsUsed) == 0 {
		t.Error("EnvVarsUsed should not be empty")
	}
}

func TestHeadAzure(t *testing.T) {
	const content = "azure blob content"
	injectBucket(t, "path/to/blob.bin", []byte(content), "application/octet-stream", map[string]string{
		"project": "calypr",
		"sha256": "beefdead",
	})

	meta, err := HeadObject(context.Background(), "az://mycontainer/path/to/blob.bin")
	if err != nil {
		t.Fatalf("HeadObject error: %v", err)
	}

	if meta.Platform != PlatformAzure {
		t.Errorf("Platform = %q, want %q", meta.Platform, PlatformAzure)
	}
	if meta.Bucket != "mycontainer" {
		t.Errorf("Bucket = %q, want mycontainer", meta.Bucket)
	}
	if meta.Key != "path/to/blob.bin" {
		t.Errorf("Key = %q, want path/to/blob.bin", meta.Key)
	}
	if meta.SizeBytes != int64(len(content)) {
		t.Errorf("SizeBytes = %d, want %d", meta.SizeBytes, len(content))
	}
	if meta.ContentType != "application/octet-stream" {
		t.Errorf("ContentType = %q, want application/octet-stream", meta.ContentType)
	}
	if got := meta.Metadata["project"]; got != "calypr" {
		t.Errorf("Metadata[project] = %q, want calypr", got)
	}
	if got := meta.Metadata["sha256"]; got != "beefdead" {
		t.Errorf("Metadata[sha256] = %q, want beefdead", got)
	}
	if len(meta.EnvVarsUsed) == 0 {
		t.Error("EnvVarsUsed should not be empty")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HeadObject dispatch
// ─────────────────────────────────────────────────────────────────────────────

func TestHeadObject_UnknownScheme(t *testing.T) {
	urls := []string{
		"ftp://server/file.bin",
		"http://unknown.example.com/file",
		"not-a-url",
	}
	for _, u := range urls {
		t.Run(u, func(t *testing.T) {
			_, err := HeadObject(context.Background(), u)
			if err == nil {
				t.Fatal("expected error for unsupported URL")
			}
			if !strings.Contains(err.Error(), "cannot detect cloud platform") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestHeadObject_BucketOpenError(t *testing.T) {
	old := openBucket
	t.Cleanup(func() { openBucket = old })
	openBucket = func(_ context.Context, _ string) (*blob.Bucket, error) {
		return nil, fmt.Errorf("injected bucket open error")
	}

	_, err := HeadObject(context.Background(), "gs://bucket/key.txt")
	if err == nil {
		t.Fatal("expected error when bucket opener fails")
	}
	if !strings.Contains(err.Error(), "injected bucket open error") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadToTempFromDRSObject_Errors(t *testing.T) {
	_, _, err := Download(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil drs object")
	}

	_, _, err = Download(context.Background(), &drs.DRSObject{})
	if err == nil {
		t.Fatal("expected error for missing access methods")
	}
}

func TestDownloadToTempFromDRSObject_Azure(t *testing.T) {
	const payload = "hello from azure object"
	injectBucket(t, "path/to/blob.bin", []byte(payload), "application/octet-stream", nil)

	drsObj := &drs.DRSObject{
		AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: "az://container/path/to/blob.bin"}}},
	}

	sha, dstPath, err := Download(context.Background(), drsObj)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(dstPath) })

	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dest file: %v", err)
	}
	if string(data) != payload {
		t.Fatalf("unexpected file content: %q", string(data))
	}

	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if sha != expected {
		t.Fatalf("sha mismatch: got %s want %s", sha, expected)
	}
}
