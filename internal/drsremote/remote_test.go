package drsremote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	sydownload "github.com/calypr/syfon/client/transfer/download"
	syfoncommon "github.com/calypr/syfon/common"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestBulkAccessURLsForObjects(t *testing.T) {
	accessID := "s3"
	methods := []drsapi.AccessMethod{{
		Type:     drsapi.AccessMethodTypeS3,
		AccessId: &accessID,
	}}
	var gotMethod, gotPath string
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(
				`{"resolved_drs_object_access_urls":[{"drs_object_id":"obj-1","drs_access_id":"s3","url":"https://signed.example/obj-1"}]}`,
			)),
			Header:  header,
			Request: r,
		}, nil
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)

	got, err := BulkAccessURLsForObjects(context.Background(), &config.GitContext{Client: client}, []drsapi.DrsObject{
		{Id: "obj-1", AccessMethods: &methods},
	})
	if err != nil {
		t.Fatalf("BulkAccessURLsForObjects returned error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/ga4gh/drs/v1/objects/access" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if got["obj-1"].Url != "https://signed.example/obj-1" {
		t.Fatalf("unexpected resolved URL: %+v", got)
	}
}

func TestFindMatchingRecord_EmptyList(t *testing.T) {
	result, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "test-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty list")
	}
}

func makeAuthzRecord(id, org, project string) drsapi.DrsObject {
	authzMap := map[string][]string{org: {project}}
	accessMethods := []drsapi.AccessMethod{{
		Type:           "s3",
		Authorizations: syfoncommon.AccessMethodAuthorizationsFromAuthzMap(authzMap),
	}}
	return drsapi.DrsObject{
		Id:            id,
		AccessMethods: &accessMethods,
		Checksums:     []drsapi.Checksum{{Type: "sha256", Checksum: "sha256"}},
	}
}

func TestFindMatchingRecord_MatchFound(t *testing.T) {
	records := []drsapi.DrsObject{
		makeAuthzRecord("no-match", "OTHER", "resource"),
		makeAuthzRecord("match", "PROG", "PROJ"),
	}

	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.Id != "match" {
		t.Fatalf("expected record id match, got %#v", result)
	}
}

func TestFindMatchingRecord_NoAuthzMatchReturnsNil(t *testing.T) {
	records := []drsapi.DrsObject{
		makeAuthzRecord("no-match", "OTHER", "resource"),
	}
	result, err := FindMatchingRecord(records, "", "PROG-PROJ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil when no authz matches, got id=%q", result.Id)
	}
}

func TestFindMatchingRecord_NonHyphenated(t *testing.T) {
	if _, err := FindMatchingRecord([]drsapi.DrsObject{}, "", "no-hyphen"); err != nil {
		t.Fatalf("FindMatchingRecord should accept non-hyphenated project ID: %v", err)
	}
}

func TestAccessURLForHashScope_FiltersByScope(t *testing.T) {
	t.Parallel()

	projectAccessID := "s3-project"
	orgAccessID := "s3-org"
	projectMethods := []drsapi.AccessMethod{{
		Type:           drsapi.AccessMethodTypeS3,
		AccessId:       &projectAccessID,
		Authorizations: syfoncommon.AccessMethodAuthorizationsFromAuthzMap(map[string][]string{"org1": {"proj1"}}),
	}}
	orgMethods := []drsapi.AccessMethod{{
		Type:           drsapi.AccessMethodTypeS3,
		AccessId:       &orgAccessID,
		Authorizations: syfoncommon.AccessMethodAuthorizationsFromAuthzMap(map[string][]string{"org1": {}}),
	}}
	checksumResponse := drsapi.N200OkDrsObjects{ResolvedDrsObject: &[]drsapi.DrsObject{
		{Id: "obj-project", Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "abc"}}, AccessMethods: &projectMethods},
		{Id: "obj-org", Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "abc"}}, AccessMethods: &orgMethods},
	}}
	checksumBody, err := json.Marshal(checksumResponse)
	if err != nil {
		t.Fatalf("marshal checksum response: %v", err)
	}

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/abc":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(checksumBody))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/obj-project/access/s3":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"url":"https://signed.example/project"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		default:
			return nil, io.EOF
		}
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)
	ctx := &config.GitContext{Client: client, Organization: "org1", ProjectId: "proj1"}

	gotObjects, err := ObjectsByHashForScope(context.Background(), ctx, "sha256:abc")
	if err != nil {
		t.Fatalf("ObjectsByHashForScope returned error: %v", err)
	}
	if len(gotObjects) != 2 {
		t.Fatalf("expected both project and org-wide matches, got %+v", gotObjects)
	}

	accessURL, obj, err := AccessURLForHashScope(context.Background(), ctx, "sha256:abc")
	if err != nil {
		t.Fatalf("AccessURLForHashScope returned error: %v", err)
	}
	if obj == nil || obj.Id != "obj-project" {
		t.Fatalf("expected project-scoped record to win, got %+v", obj)
	}
	if accessURL == nil || accessURL.Url != "https://signed.example/project" {
		t.Fatalf("unexpected access URL: %+v", accessURL)
	}
}

func TestDownloadResolvedToPath_RangeIgnoredRestartsDownload(t *testing.T) {
	t.Parallel()

	payload := []byte("abcdefghijklmnopqrstuvwxyz")
	var rangeRequests int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/download/object.bin" {
			return nil, io.EOF
		}
		if r.Header.Get("Range") != "" {
			rangeRequests++
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(payload))),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)
	drsCtx := &config.GitContext{Client: client}

	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "cache", "object.bin")
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dstPath, payload[:10], 0o644); err != nil {
		t.Fatalf("seed partial download: %v", err)
	}

	obj := &drsapi.DrsObject{Id: "obj-1", Size: int64(len(payload))}
	accessURL := &drsapi.AccessURL{Url: "https://signed.example/download/object.bin"}
	err = DownloadResolvedToPath(context.Background(), drsCtx, "obj-1", dstPath, obj, accessURL, sydownload.DownloadOptions{
		MultipartThreshold: int64(len(payload) + 1),
		Concurrency:        2,
		ChunkSize:          8,
	})
	if err != nil {
		t.Fatalf("DownloadResolvedToPath returned error: %v", err)
	}

	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unexpected payload: got %q want %q", string(got), string(payload))
	}
	if rangeRequests == 0 {
		t.Fatal("expected downloader to attempt a range request before restarting")
	}
}
