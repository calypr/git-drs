package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestNewValidatesConnectionOptions(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected missing endpoint error")
	}
	if _, err := New(Options{Endpoint: "https://example.test"}); err == nil {
		t.Fatal("expected missing project error")
	}
	if _, err := New(Options{Endpoint: "https://example.test", Project: "project"}); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestSafeRelativePath(t *testing.T) {
	for _, path := range []string{"/absolute", "../outside", "..", "."} {
		if _, err := safeRelativePath(path); err == nil {
			t.Fatalf("expected unsafe path %q to fail", path)
		}
	}
	if got, err := safeRelativePath("data/file.bin"); err != nil || got != "data/file.bin" {
		t.Fatalf("safe path = %q, err = %v", got, err)
	}
}

func TestPullUsesBulkAccessBeforePerObjectAccess(t *testing.T) {
	payload := []byte("hydrated")
	digest := sha256.Sum256(payload)
	oid := hex.EncodeToString(digest[:])
	accessID := "s3"
	methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &accessID}}
	controlled := []string{"/organization/org/project/proj"}
	object := drsapi.DrsObject{
		Id:               "object-1",
		Size:             int64(len(payload)),
		ControlledAccess: &controlled,
		Checksums:        []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
		AccessMethods:    &methods,
	}
	checksumResponse, err := json.Marshal(drsapi.N200OkDrsObjects{ResolvedDrsObject: &[]drsapi.DrsObject{object}})
	if err != nil {
		t.Fatal(err)
	}

	bulkCalled := false
	singleCalled := false
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/"+oid:
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(checksumResponse))), Request: r}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/ga4gh/drs/v1/objects/access":
			bulkCalled = true
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(`{"resolved_drs_object_access_urls":[{"drs_object_id":"object-1","drs_access_id":"s3","url":"http://signed.example/object-1"}]}`)), Request: r}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/object-1/access/s3":
			singleCalled = true
			return nil, io.EOF
		case r.Method == http.MethodGet && r.URL.Host == "signed.example" && r.URL.Path == "/object-1":
			return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(payload))), Request: r}, nil
		default:
			return nil, io.EOF
		}
	})}

	client, err := New(Options{
		Endpoint:     "http://example.test",
		AccessToken:  "token",
		Organization: "org",
		Project:      "proj",
		HTTPClient:   httpClient,
	})
	if err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "data.bin")
	if err := client.Pull(context.Background(), PullOptions{
		Root:  filepath.Dir(dst),
		Files: []File{{Path: filepath.Base(dst), OID: oid, Size: int64(len(payload))}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded payload = %q, want %q", got, payload)
	}
	if !bulkCalled || singleCalled {
		t.Fatalf("bulkCalled=%v singleCalled=%v, want bulk only", bulkCalled, singleCalled)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
