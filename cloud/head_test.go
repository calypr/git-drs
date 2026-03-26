package cloud

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/calypr/data-client/drs"
)

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
			account, container, blob, err := parseAzureURL(tt.rawURL)
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
			if blob != tt.blob {
				t.Errorf("blob = %q, want %q", blob, tt.blob)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseAzureConnString
// ─────────────────────────────────────────────────────────────────────────────

func TestParseAzureConnString(t *testing.T) {
	tests := []struct {
		name       string
		connString string
		wantAcct   string
		wantKey    string
	}{
		{
			name:       "standard connection string",
			connString: "DefaultEndpointsProtocol=https;AccountName=myaccount;AccountKey=bXlrZXk=;EndpointSuffix=core.windows.net",
			wantAcct:   "myaccount",
			wantKey:    "bXlrZXk=",
		},
		{
			name:       "key with padding equals signs",
			connString: "AccountName=acct;AccountKey=abc123==",
			wantAcct:   "acct",
			wantKey:    "abc123==",
		},
		{
			name:       "key only",
			connString: "AccountKey=key123",
			wantAcct:   "",
			wantKey:    "key123",
		},
		{
			name:       "empty string",
			connString: "",
			wantAcct:   "",
			wantKey:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account, key := parseAzureConnString(tt.connString)
			if account != tt.wantAcct {
				t.Errorf("account = %q, want %q", account, tt.wantAcct)
			}
			if key != tt.wantKey {
				t.Errorf("key = %q, want %q", key, tt.wantKey)
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

	meta, err := headS3(context.Background(), "s3://test-bucket/path/to/file.bin")
	if err != nil {
		t.Fatalf("headS3 error: %v", err)
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

			_, err := headS3(context.Background(), "s3://bucket/key.txt")
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
	// Generate a 2048-bit RSA key for the fake service account JWT signing.
	// The google auth library requires a valid RSA key; key size is not validated.
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	privPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}))

	// Single mock server serves both the OAuth2 token endpoint (/token) and
	// the GCS JSON API (/storage/v1/b/{bucket}/o/{key}).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			// Fake OAuth2 JWT bearer token response.
			json.NewEncoder(w).Encode(map[string]any{
				"access_token": "fake-gcs-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
			return
		}
		// GCS JSON API: return fake object metadata.
		json.NewEncoder(w).Encode(gcsObjectResponse{
			Bucket:      "gcs-bucket",
			Name:        "path/to/data.bin",
			Size:        "1024",
			ETag:        "gcs-etag-xyz",
			ContentType: "application/octet-stream",
			Updated:     time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC),
			Metadata:    map[string]string{"project": "calypr", "sha256": "cafebabe"},
		})
	}))
	defer srv.Close()

	// Write service account JSON to a temp file; point credentials at it.
	credFile, err := os.CreateTemp("", "gcs-test-creds-*.json")
	if err != nil {
		t.Fatalf("create temp cred file: %v", err)
	}
	t.Cleanup(func() { os.Remove(credFile.Name()) })
	if _, err := credFile.Write(buildGCSServiceAccountJSON(t, srv.URL+"/token", privPEM)); err != nil {
		t.Fatalf("write cred file: %v", err)
	}
	credFile.Close()

	t.Setenv(GCSCredentialsEnvVar, credFile.Name())

	// Redirect GCS API calls to the mock server.
	oldBase := gcsAPIBase
	gcsAPIBase = srv.URL
	t.Cleanup(func() { gcsAPIBase = oldBase })

	meta, err := headGCS(context.Background(), "gs://gcs-bucket/path/to/data.bin")
	if err != nil {
		t.Fatalf("headGCS error: %v", err)
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
	if meta.SizeBytes != 1024 {
		t.Errorf("SizeBytes = %d, want 1024", meta.SizeBytes)
	}
	if meta.ETag != "gcs-etag-xyz" {
		t.Errorf("ETag = %q, want gcs-etag-xyz", meta.ETag)
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

func TestHeadGCS_InvalidCredentials(t *testing.T) {
	// A file that is not valid credential JSON must produce a parse error.
	credFile, err := os.CreateTemp("", "gcs-bad-creds-*.json")
	if err != nil {
		t.Fatalf("create temp cred file: %v", err)
	}
	t.Cleanup(func() { os.Remove(credFile.Name()) })
	credFile.WriteString("not valid json at all")
	credFile.Close()

	t.Setenv(GCSCredentialsEnvVar, credFile.Name())

	_, err = headGCS(context.Background(), "gs://bucket/key.txt")
	if err == nil {
		t.Fatal("expected error for invalid credentials file")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Azure
// ─────────────────────────────────────────────────────────────────────────────

func TestHeadAzure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("Content-Length", "2048")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("ETag", `"azure-etag-abc"`)
		w.Header().Set("Last-Modified", "Mon, 24 Mar 2026 12:00:00 GMT")
		w.Header().Set("x-ms-meta-project", "calypr")
		w.Header().Set("x-ms-meta-sha256", "beefdead")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Use SAS token auth to avoid HMAC signing.
	t.Setenv(AzureAccountEnvVar, "testaccount")
	t.Setenv(AzureSASTokenEnvVar, "sv=2020-08-04&se=2099-01-01T00:00:00Z")
	t.Setenv(AzureKeyEnvVar, "")

	// Redirect Azure blob endpoint to the mock server.
	oldBase := azureBlobBase
	azureBlobBase = srv.URL
	t.Cleanup(func() { azureBlobBase = oldBase })

	meta, err := headAzure(context.Background(), "az://mycontainer/path/to/blob.bin")
	if err != nil {
		t.Fatalf("headAzure error: %v", err)
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
	if meta.SizeBytes != 2048 {
		t.Errorf("SizeBytes = %d, want 2048", meta.SizeBytes)
	}
	if meta.ETag != "azure-etag-abc" {
		t.Errorf("ETag = %q, want azure-etag-abc", meta.ETag)
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

func TestHeadAzure_MissingAccount(t *testing.T) {
	t.Setenv(AzureAccountEnvVar, "")
	t.Setenv(AzureKeyEnvVar, "")
	t.Setenv(AzureSASTokenEnvVar, "")
	t.Setenv(AzureConnStringEnvVar, "")

	_, err := headAzure(context.Background(), "az://container/blob.txt")
	if err == nil {
		t.Fatal("expected error when account is missing")
	}
	if !strings.Contains(err.Error(), AzureAccountEnvVar) {
		t.Errorf("error should mention %s, got: %v", AzureAccountEnvVar, err)
	}
}

func TestHeadAzure_MissingAuth(t *testing.T) {
	t.Setenv(AzureAccountEnvVar, "myaccount")
	t.Setenv(AzureKeyEnvVar, "")
	t.Setenv(AzureSASTokenEnvVar, "")
	t.Setenv(AzureConnStringEnvVar, "")

	_, err := headAzure(context.Background(), "az://container/blob.txt")
	if err == nil {
		t.Fatal("expected error when no auth method is set")
	}
	if !strings.Contains(err.Error(), AzureKeyEnvVar) {
		t.Errorf("error should mention %s, got: %v", AzureKeyEnvVar, err)
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

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.Header().Set("ETag", `"etag-123"`)
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(payload))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	t.Setenv(AzureAccountEnvVar, "testaccount")
	t.Setenv(AzureSASTokenEnvVar, "sv=2020-08-04&se=2099-01-01T00:00:00Z")
	t.Setenv(AzureKeyEnvVar, "")

	oldBase := azureBlobBase
	azureBlobBase = srv.URL
	t.Cleanup(func() { azureBlobBase = oldBase })

	drsObj := &drs.DRSObject{
		AccessMethods: []drs.AccessMethod{
			{AccessURL: drs.AccessURL{URL: "az://container/path/to/blob.bin"}},
		},
	}

	sha, tmpPath, err := Download(context.Background(), drsObj)
	if err != nil {
		t.Fatalf("Download error: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(tmpPath) })

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}
	if string(data) != payload {
		t.Fatalf("unexpected temp file content: %q", string(data))
	}

	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	if sha != expected {
		t.Fatalf("sha mismatch: got %s want %s", sha, expected)
	}
}
