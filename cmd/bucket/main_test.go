package bucket

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	bucketapi "github.com/calypr/syfon/apigen/bucketapi"
)

type bucketRoundTripFunc func(*http.Request) (*http.Response, error)

func (f bucketRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestAddServerBucketScopeUsesSyfonClient(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "ok", statusCode: http.StatusOK},
		{name: "created", statusCode: http.StatusCreated},
		{name: "rejected", statusCode: http.StatusForbidden, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotMethod        string
				gotURL           string
				gotPath          string
				gotAuthorization string
				gotContentType   string
				gotBody          map[string]any
				readErr          error
				decodeErr        error
			)
			var (
				gotStatus   = tc.statusCode
				gotBodyText string
			)
			oldHTTPClient := newBucketHTTPClient
			newBucketHTTPClient = func() *http.Client {
				return &http.Client{Timeout: defaultBucketAPITimeout, Transport: bucketRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotMethod = r.Method
					gotURL = r.URL.String()
					gotPath = r.URL.Path
					gotAuthorization = r.Header.Get("Authorization")
					gotContentType = r.Header.Get("Content-Type")

					body, err := io.ReadAll(r.Body)
					if err != nil {
						readErr = err
					} else {
						decodeErr = json.Unmarshal(body, &gotBody)
					}

					if tc.wantErr {
						gotBodyText = `{"message":"scope denied"}`
					}
					return &http.Response{
						StatusCode: gotStatus,
						Status:     http.StatusText(gotStatus),
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(gotBodyText)),
						Request:    r,
					}, nil
				})}
			}
			t.Cleanup(func() { newBucketHTTPClient = oldHTTPClient })

			var err error
			err = addServerBucketScope(
				context.Background(),
				"https://syfon.example.test/api",
				"test-token",
				"data-bucket",
				addBucketScopePayload{
					Organization: "example-org",
					ProjectID:    "project-1",
					Path:         "s3://data-bucket/program/project",
				},
			)
			if tc.wantErr {
				if err == nil {
					t.Fatal("addServerBucketScope returned nil error")
				}
				if !strings.Contains(err.Error(), "status 403") || !strings.Contains(err.Error(), "scope denied") {
					t.Fatalf("addServerBucketScope error = %v, want status and response message", err)
				}
			} else if err != nil {
				t.Fatalf("addServerBucketScope returned error: %v", err)
			}

			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			if decodeErr != nil {
				t.Fatalf("decode request body: %v", decodeErr)
			}
			if gotMethod != http.MethodPost {
				t.Errorf("request method = %q, want %q", gotMethod, http.MethodPost)
			}
			if gotURL != "https://syfon.example.test/api/data/buckets/data-bucket/scopes" {
				t.Errorf("request URL = %q, want %q", gotURL, "https://syfon.example.test/api/data/buckets/data-bucket/scopes")
			}
			if gotPath != "/api/data/buckets/data-bucket/scopes" {
				t.Errorf("request path = %q, want %q", gotPath, "/api/data/buckets/data-bucket/scopes")
			}
			if gotAuthorization != "Bearer test-token" {
				t.Errorf("authorization = %q, want %q", gotAuthorization, "Bearer test-token")
			}
			if gotContentType != "application/json" {
				t.Errorf("content type = %q, want %q", gotContentType, "application/json")
			}
			if got := gotBody["organization"]; got != "example-org" {
				t.Errorf("organization = %v, want %q", got, "example-org")
			}
			if got := gotBody["project_id"]; got != "project-1" {
				t.Errorf("project_id = %v, want %q", got, "project-1")
			}
			if got := gotBody["path"]; got != "s3://data-bucket/program/project" {
				t.Errorf("path = %v, want %q", got, "s3://data-bucket/program/project")
			}
		})
	}
}

func TestUpsertServerBucketUsesSyfonClient(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{name: "ok", statusCode: http.StatusOK},
		{name: "created", statusCode: http.StatusCreated},
		{name: "rejected", statusCode: http.StatusBadRequest, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotMethod        string
				gotURL           string
				gotPath          string
				gotAuthorization string
				gotContentType   string
				gotBody          map[string]any
				readErr          error
				decodeErr        error
			)
			oldHTTPClient := newBucketHTTPClient
			newBucketHTTPClient = func() *http.Client {
				return &http.Client{Timeout: defaultBucketAPITimeout, Transport: bucketRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					gotMethod = r.Method
					gotURL = r.URL.String()
					gotPath = r.URL.Path
					gotAuthorization = r.Header.Get("Authorization")
					gotContentType = r.Header.Get("Content-Type")

					body, err := io.ReadAll(r.Body)
					if err != nil {
						readErr = err
					} else {
						decodeErr = json.Unmarshal(body, &gotBody)
					}

					bodyText := ""
					if tc.wantErr {
						bodyText = `{"message":"credential denied"}`
					}
					return &http.Response{
						StatusCode: tc.statusCode,
						Status:     http.StatusText(tc.statusCode),
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(bodyText)),
						Request:    r,
					}, nil
				})}
			}
			t.Cleanup(func() { newBucketHTTPClient = oldHTTPClient })

			region := "us-east-1"
			accessKey := "access-key"
			secretKey := "secret-key"
			endpoint := "https://s3.example.test"
			err := upsertServerBucket(
				context.Background(),
				"https://syfon.example.test/api",
				"test-token",
				bucketapi.PutBucketRequest{
					Bucket:    "data-bucket",
					Region:    &region,
					AccessKey: &accessKey,
					SecretKey: &secretKey,
					Endpoint:  &endpoint,
				},
			)
			if tc.wantErr {
				if err == nil {
					t.Fatal("upsertServerBucket returned nil error")
				}
				if !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "credential denied") {
					t.Fatalf("upsertServerBucket error = %v, want status and response message", err)
				}
			} else if err != nil {
				t.Fatalf("upsertServerBucket returned error: %v", err)
			}

			if readErr != nil {
				t.Fatalf("read request body: %v", readErr)
			}
			if decodeErr != nil {
				t.Fatalf("decode request body: %v", decodeErr)
			}
			if gotMethod != http.MethodPut {
				t.Errorf("request method = %q, want %q", gotMethod, http.MethodPut)
			}
			if gotURL != "https://syfon.example.test/api/data/buckets" {
				t.Errorf("request URL = %q, want %q", gotURL, "https://syfon.example.test/api/data/buckets")
			}
			if gotPath != "/api/data/buckets" {
				t.Errorf("request path = %q, want %q", gotPath, "/api/data/buckets")
			}
			if gotAuthorization != "Bearer test-token" {
				t.Errorf("authorization = %q, want %q", gotAuthorization, "Bearer test-token")
			}
			if gotContentType != "application/json" {
				t.Errorf("content type = %q, want %q", gotContentType, "application/json")
			}
			for field, want := range map[string]string{
				"bucket":     "data-bucket",
				"region":     "us-east-1",
				"access_key": "access-key",
				"secret_key": "secret-key",
				"endpoint":   "https://s3.example.test",
			} {
				if got := gotBody[field]; got != want {
					t.Errorf("%s = %v, want %q", field, got, want)
				}
			}
			for field := range map[string]struct{}{"organization": {}, "project_id": {}} {
				if got, ok := gotBody[field]; !ok || got != "" {
					t.Errorf("%s = %v, want present empty string", field, got)
				}
			}
		})
	}
}

func TestBucketFromStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "s3",
			path: "s3://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name: "gcs",
			path: "gs://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name: "azure",
			path: "azblob://data-bucket/program/project",
			want: "data-bucket",
		},
		{
			name:    "missing scheme",
			path:    "data-bucket/program/project",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			path:    "s3:///program/project",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bucketFromStoragePath(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("bucketFromStoragePath(%q) returned nil error", tc.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("bucketFromStoragePath(%q) returned error: %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("bucketFromStoragePath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestNormalizeStoragePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		bucket  string
		want    string
		wantErr bool
	}{
		{
			name:   "root bucket path",
			path:   "s3://data-bucket",
			bucket: "data-bucket",
			want:   "",
		},
		{
			name:   "program path",
			path:   "s3://data-bucket/program-root",
			bucket: "data-bucket",
			want:   "program-root",
		},
		{
			name:   "project path",
			path:   "s3://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:   "gcs path",
			path:   "gs://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:   "azure path",
			path:   "azblob://data-bucket/program-root/project-subpath",
			bucket: "data-bucket",
			want:   "program-root/project-subpath",
		},
		{
			name:    "bucket mismatch",
			path:    "s3://other-bucket/program-root",
			bucket:  "data-bucket",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			path:    "https://data-bucket/program-root",
			bucket:  "data-bucket",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeStoragePath(tc.path, tc.bucket)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizeStoragePath(%q, %q) returned nil error", tc.path, tc.bucket)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeStoragePath(%q, %q) returned error: %v", tc.path, tc.bucket, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeStoragePath(%q, %q) = %q, want %q", tc.path, tc.bucket, got, tc.want)
			}
		})
	}
}
