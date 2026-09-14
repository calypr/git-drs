package addurl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/remoteruntime"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	syclient "github.com/calypr/syfon/client"
	"github.com/calypr/syfon/client/apierror"
)

func TestInspectRemoteObjectViaServerMapsSyfonResponse(t *testing.T) {
	const (
		sourceURL    = "s3://bucket/path/to/file.bin"
		objectURL    = "s3://bucket/path/to/file.bin?versionId=abc"
		lastModified = "2026-09-13T12:34:56Z"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/data/inspect" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}

		var request internalapi.InternalInspectObjectRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ObjectUrl != sourceURL {
			t.Errorf("object URL = %q, want %q", request.ObjectUrl, sourceURL)
		}
		if request.Organization != "" || request.Project != "" || request.Key != "" || request.Scheme != "" {
			t.Errorf("unexpected key-mode fields in URL request: %+v", request)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object_url":    objectURL,
			"provider":      "s3",
			"bucket":        "bucket",
			"key":           "path/to/file.bin",
			"path":          "file.bin",
			"size_bytes":    11,
			"meta_sha256":   "sha256-value",
			"etag":          "etag-value",
			"last_modified": lastModified,
		})
	}))
	defer server.Close()

	client, err := syclient.New(server.URL, syclient.WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("new Syfon client: %v", err)
	}

	got, err := inspectRemoteObjectViaServer(
		context.Background(),
		&remoteruntime.GitContext{Client: client},
		addURLInput{sourceArg: sourceURL},
	)
	if err != nil {
		t.Fatalf("inspectRemoteObjectViaServer returned error: %v", err)
	}

	wantTime := time.Date(2026, time.September, 13, 12, 34, 56, 0, time.UTC)
	if got.objectURL != objectURL {
		t.Fatalf("object URL = %q, want %q", got.objectURL, objectURL)
	}
	if got.info == nil {
		t.Fatal("expected object info")
	}
	if got.info.Bucket != "bucket" || got.info.Key != "path/to/file.bin" || got.info.Path != "file.bin" {
		t.Fatalf("unexpected object identity: %+v", got.info)
	}
	if got.info.SizeBytes != 11 || got.info.MetaSHA256 != "sha256-value" || got.info.ETag != "etag-value" {
		t.Fatalf("unexpected object metadata: %+v", got.info)
	}
	if !got.info.LastModTime.Equal(wantTime) {
		t.Fatalf("last modified = %s, want %s", got.info.LastModTime, wantTime)
	}
}

func TestMapInspectError_UpgradeMessageForMissingRoute(t *testing.T) {
	err := mapInspectError("s3://bucket/key", &apierror.APIError{
		Method: http.MethodPost,
		URL:    "https://example.test/data/inspect",
		Status: http.StatusNotFound,
		Body:   "Cannot POST /data/inspect",
	})
	if err == nil || !strings.Contains(err.Error(), "upgrade Syfon") {
		t.Fatalf("expected upgrade message, got %v", err)
	}
}

func TestMapInspectError_ActionableForbidden(t *testing.T) {
	err := mapInspectError("s3://bucket/key", &apierror.APIError{
		Method: http.MethodPost,
		URL:    "https://example.test/data/inspect",
		Status: http.StatusForbidden,
		Body:   "provider denied access to s3://bucket/key",
	})
	if err == nil || !strings.Contains(err.Error(), "was denied") {
		t.Fatalf("expected denied message, got %v", err)
	}
}
