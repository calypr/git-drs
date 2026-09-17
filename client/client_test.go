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

	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
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

func TestPullRejectsUnsafePathsBeforeDownloadOrWrite(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "pull")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}

	requests := 0
	client, err := New(Options{
		Endpoint:    "http://example.test",
		AccessToken: "token",
		Project:     "project",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return nil, io.EOF
		})},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/absolute", want: `path must be relative to pull root: "/absolute"`},
		{path: "../outside", want: `path must be relative to pull root: "../outside"`},
		{path: "..", want: `path must be relative to pull root: ".."`},
		{path: ".", want: `path must be relative to pull root: "."`},
	} {
		requests = 0
		err := client.Pull(context.Background(), PullOptions{
			Root: root,
			Files: []File{{
				Path: test.path,
				OID:  "sha256:" + strings.Repeat("0", 64),
				Size: 0,
			}},
		})
		if err == nil || err.Error() != test.want {
			t.Fatalf("Pull(%q) error = %v, want %q", test.path, err, test.want)
		}
		if requests != 0 {
			t.Fatalf("Pull(%q) made %d download requests", test.path, requests)
		}
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != "pull" {
			t.Fatalf("Pull(%q) changed destination tree: %v", test.path, entries)
		}
		rootEntries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(rootEntries) != 0 {
			t.Fatalf("Pull(%q) wrote to pull root: %v", test.path, rootEntries)
		}
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
	checksumResponse, err := json.Marshal(internalapi.BulkHashesResponse{Results: map[string][]internalapi.InternalRecord{
		oid: {{
			Did:              object.Id,
			Size:             &object.Size,
			ControlledAccess: object.ControlledAccess,
			Hashes:           &internalapi.HashInfo{"sha256": oid},
			AccessMethods:    object.AccessMethods,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	bulkCalled := false
	singleCalled := false
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/index/bulk/hashes":
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

	root := t.TempDir()
	dst := filepath.Join(root, "data", "file.bin")
	if err := client.Pull(context.Background(), PullOptions{
		Root:  root,
		Files: []File{{Path: filepath.Join("data", "file.bin"), OID: oid, Size: int64(len(payload))}},
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
