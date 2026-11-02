package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

func TestPullRejectsSymlinkedParentOutsideRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "pull")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skipf("symlink creation is unavailable: %v", err)
	}

	payload := []byte("downloaded outside root")
	client, requests := newPullTestClient(t, payload, payload)
	err := client.Pull(context.Background(), PullOptions{
		Root: root,
		Files: []File{{
			Path: "linked/escaped.bin",
			OID:  sha256OID(payload),
			Size: int64(len(payload)),
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "escapes pull root") {
		t.Fatalf("Pull through symlink error = %v, want a root escape error", err)
	}
	if *requests != 0 {
		t.Fatalf("Pull made %d download requests for an escaping path", *requests)
	}
	if _, err := os.Lstat(filepath.Join(outside, "escaped.bin")); !os.IsNotExist(err) {
		t.Fatalf("outside destination stat error = %v, want not exist", err)
	}
}

func TestPullOverwriteFailurePreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "data.bin")
	original := []byte("valuable local data")
	if err := os.WriteFile(dst, original, 0o600); err != nil {
		t.Fatal(err)
	}

	expected := []byte("correct payload")
	damaged := []byte("damaged payload")
	client, _ := newPullTestClient(t, expected, damaged)
	err := client.Pull(context.Background(), PullOptions{
		Root:      root,
		Overwrite: true,
		Files: []File{{
			Path: "data.bin",
			OID:  sha256OID(expected),
			Size: int64(len(expected)),
		}},
	})
	if err == nil {
		t.Fatal("Pull with a bad checksum succeeded")
	}
	got, readErr := os.ReadFile(dst)
	if readErr != nil {
		t.Fatalf("read original destination after failed Pull: %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("destination after failed Pull = %q, want original %q", got, original)
	}
}

func TestPullOverwriteReplacesFileAndPreservesMode(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "data.bin")
	if err := os.WriteFile(dst, []byte("old payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	payload := []byte("new payload")
	client, _ := newPullTestClient(t, payload, payload)
	if err := client.Pull(context.Background(), PullOptions{
		Root:      root,
		Overwrite: true,
		Files: []File{{
			Path: "data.bin",
			OID:  sha256OID(payload),
			Size: int64(len(payload)),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("overwritten destination = %q, want %q", got, payload)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("overwritten destination mode = %04o, want %04o", got, want)
	}
}

func TestPullPreservesWhitespaceInRequestedPath(t *testing.T) {
	root := t.TempDir()
	payload := []byte("hydrated")
	client, _ := newPullTestClient(t, payload, payload)
	requestedPath := " leading.bin "
	if err := client.Pull(context.Background(), PullOptions{
		Root: root,
		Files: []File{{
			Path: requestedPath,
			OID:  sha256OID(payload),
			Size: int64(len(payload)),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(root, requestedPath))
	if err != nil {
		t.Fatalf("read requested destination: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("requested destination = %q, want %q", got, payload)
	}
	if _, err := os.Lstat(filepath.Join(root, strings.TrimSpace(requestedPath))); !os.IsNotExist(err) {
		t.Fatalf("trimmed destination stat error = %v, want not exist", err)
	}
}

func newPullTestClient(t *testing.T, expected, downloaded []byte) (*Client, *int) {
	t.Helper()

	oid := sha256OID(expected)
	accessID := "s3"
	controlled := []string{"/organization/org/project/proj"}
	methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &accessID}}
	object := drsapi.DrsObject{
		Id:               "object-1",
		Size:             int64(len(expected)),
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

	requests := 0
	client, err := New(Options{
		Endpoint:     "http://example.test",
		AccessToken:  "token",
		Organization: "org",
		Project:      "proj",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			requests++
			header := make(http.Header)
			header.Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/index/bulk/hashes":
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(checksumResponse))), Request: r}, nil
			case r.Method == http.MethodPost && r.URL.Path == "/ga4gh/drs/v1/objects/access":
				body := `{"resolved_drs_object_access_urls":[{"drs_object_id":"object-1","drs_access_id":"s3","url":"http://signed.example/object-1"}]}`
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			case r.Method == http.MethodGet && r.URL.Host == "signed.example" && r.URL.Path == "/object-1":
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader(string(downloaded))), Request: r}, nil
			default:
				return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
			}
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	return client, &requests
}

func sha256OID(payload []byte) string {
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
