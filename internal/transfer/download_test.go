package transfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	syclient "github.com/calypr/syfon/client"
	sytransfer "github.com/calypr/syfon/client/transfer"
	sydownload "github.com/calypr/syfon/client/transfer/download"
)

type downloadRoundTripFunc func(*http.Request) (*http.Response, error)

func (f downloadRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type immediateDownloadRetry struct{}

func (immediateDownloadRetry) WaitTime(int) time.Duration { return 0 }

func TestDownloadToCachePathDispatchesDRSOIDToURIResolver(t *testing.T) {
	t.Parallel()

	for _, oid := range []string{"drs://example.test/object-1", "//example.test/object-1"} {
		oid := oid
		t.Run(oid, func(t *testing.T) {
			t.Parallel()

			var requestPath string
			httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				requestPath = r.URL.Path
				return nil, io.EOF
			})}
			raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
			if err != nil {
				t.Fatalf("syclient.New: %v", err)
			}

			err = DownloadToCachePath(context.Background(), &remoteruntime.GitContext{Client: raw}, oid, filepath.Join(t.TempDir(), "object"))
			if err == nil {
				t.Fatal("DownloadToCachePath unexpectedly succeeded")
			}
			if strings.Contains(requestPath, "/checksum/") {
				t.Fatalf("DRS OID was sent to checksum lookup: %s", requestPath)
			}
			if !strings.Contains(requestPath, "/objects/drs://example.test/object-1") {
				t.Fatalf("DRS OID was not sent to URI lookup: %s", requestPath)
			}
		})
	}
}

func TestBulkAccessURLsForObjects(t *testing.T) {
	accessID := "s3"
	methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &accessID}}
	var gotMethod, gotPath string
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		header := make(http.Header)
		header.Set("Content-Type", "application/json")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"url":"https://signed.example/obj-1"}`)),
			Header:     header,
			Request:    r,
		}, nil
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw

	got, err := BulkAccessURLsForObjects(context.Background(), &remoteruntime.GitContext{Client: client}, []drsapi.DrsObject{
		{Id: "obj-1", AccessMethods: &methods},
	})
	if err != nil {
		t.Fatalf("BulkAccessURLsForObjects returned error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("expected GET, got %s", gotMethod)
	}
	if gotPath != "/ga4gh/drs/v1/objects/obj-1/access/s3" {
		t.Fatalf("unexpected path: %s", gotPath)
	}
	if got["obj-1"].Url != "https://signed.example/obj-1" {
		t.Fatalf("unexpected resolved URL: %+v", got)
	}
}

func TestVerifyGlobusDownloadRejectsWrongContent(t *testing.T) {
	payload := []byte("expected")
	want := sha256.Sum256(payload)
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("incorrect"), 0o644); err != nil {
		t.Fatal(err)
	}
	obj := &drsapi.DrsObject{
		Size:      int64(len("incorrect")),
		Checksums: []drsapi.Checksum{{Type: "sha-256", Checksum: hex.EncodeToString(want[:])}},
	}
	if err := verifyGlobusDownload(path, "drs://example.org/object", obj, false); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if err := verifyGlobusDownload(path, strings.Repeat("a", 64), obj, true); err == nil {
		t.Fatal("expected placeholder download to use the published checksum")
	}
}

func TestVerifyGlobusDownloadDetectsPlaceholderChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	oid := strings.Repeat("a", 64)
	obj := &drsapi.DrsObject{Size: int64(len("payload")), Checksums: []drsapi.Checksum{{Type: "git-drs-placeholder", Checksum: oid}}}
	if err := verifyGlobusDownload(path, oid, obj, false); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadGlobusResolvedRemovesPartialTransfer(t *testing.T) {
	dstPath := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(dstPath, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := downloadGlobusResolved(context.Background(), nil, "invalid", dstPath, "oid", &drsapi.DrsObject{}); err == nil {
		t.Fatal("expected transfer failure")
	}
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		t.Fatalf("partial destination remains: %v", err)
	}
}

func TestAccessURLForHashScopeFiltersByScope(t *testing.T) {
	t.Parallel()

	projectAccessID := "s3-project"
	orgAccessID := "s3-org"
	projectMethods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &projectAccessID}}
	projectMethods[0].AccessUrl = &drsapi.AccessURL{Url: "s3://bucket/object"}
	orgMethods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &orgAccessID}}
	projectControlled := []string{"/organization/org1/project/proj1"}
	orgControlled := []string{"/organization/org1"}
	abcHashes := internalapi.HashInfo{"sha256": "abc"}
	checksumResponse := struct {
		Results map[string][]internalapi.InternalRecord `json:"results"`
	}{Results: map[string][]internalapi.InternalRecord{"abc": {
		{Did: "obj-project", ControlledAccess: &projectControlled, Hashes: &abcHashes, AccessMethods: &projectMethods},
		{Did: "obj-org", ControlledAccess: &orgControlled, Hashes: &abcHashes, AccessMethods: &orgMethods},
	}}}
	checksumBody, err := json.Marshal(checksumResponse)
	if err != nil {
		t.Fatalf("marshal checksum response: %v", err)
	}

	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/index/bulk/hashes":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(checksumBody))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/obj-project/access/s3-project":
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
	client := raw
	ctx := &remoteruntime.GitContext{Client: client, Organization: "org1", ProjectId: "proj1"}

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

func TestDownloadResolvedToPathRangeIgnoredRestartsDownload(t *testing.T) {
	t.Parallel()

	payload := []byte("abcdefghijklmnopqrstuvwxyz")
	var rangeRequests int
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/download/object.bin" {
			return nil, io.EOF
		}
		if r.Header.Get("X-Provider-Token") != "secret" {
			t.Fatalf("missing access URL header: %v", r.Header)
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
	client := raw
	drsCtx := &remoteruntime.GitContext{Client: client}

	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "cache", "object.bin")
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dstPath, payload[:10], 0o644); err != nil {
		t.Fatalf("seed partial download: %v", err)
	}
	checkpoint, err := json.Marshal(struct {
		Identity string `json:"identity"`
		Size     int64  `json:"size"`
	}{Identity: "obj-1", Size: int64(len(payload))})
	if err != nil {
		t.Fatalf("marshal download checkpoint: %v", err)
	}
	if err := os.WriteFile(dstPath+".syfon-download.json", checkpoint, 0o644); err != nil {
		t.Fatalf("seed download checkpoint: %v", err)
	}

	obj := &drsapi.DrsObject{Id: "obj-1", Size: int64(len(payload))}
	headers := []string{"X-Provider-Token: secret"}
	accessURL := &drsapi.AccessURL{Url: "https://signed.example/download/object.bin", Headers: &headers}
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

func TestDownloadResolvedToPathReturnsHTTPErrorBeforeWritingBody(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/download/object.bin" {
			return nil, io.EOF
		}
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Body:       io.NopCloser(strings.NewReader("<html><body><h1>403 Forbidden</h1></body></html>")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw
	drsCtx := &remoteruntime.GitContext{Client: client}

	tmpDir := t.TempDir()
	dstPath := filepath.Join(tmpDir, "cache", "object.bin")
	obj := &drsapi.DrsObject{Id: "obj-1", Size: 685585}
	accessURL := &drsapi.AccessURL{Url: "https://signed.example/download/object.bin"}

	err = DownloadResolvedToPath(context.Background(), drsCtx, "obj-1", dstPath, obj, accessURL, sydownload.DownloadOptions{
		MultipartThreshold: 685586,
		Concurrency:        2,
		ChunkSize:          8,
	})
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "download from https://signed.example/download/object.bin failed") {
		t.Fatalf("expected download URL in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Forbidden") {
		t.Fatalf("expected response body in error, got %v", err)
	}
	if _, statErr := os.Stat(dstPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no downloaded file to be written, got stat err=%v", statErr)
	}
	if _, statErr := os.Stat(dstPath + ".syfon-download.json"); !os.IsNotExist(statErr) {
		t.Fatalf("expected no download checkpoint to be written, got stat err=%v", statErr)
	}
}

func TestDownloadResolvedToPathRetriesTransientHTTPError(t *testing.T) {
	t.Parallel()

	payload := "downloaded"
	requests := 0
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		status := http.StatusServiceUnavailable
		body := "temporarily unavailable"
		if requests > 1 {
			status = http.StatusOK
			body = payload
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}

	client, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	dstPath := filepath.Join(t.TempDir(), "object.bin")
	err = DownloadResolvedToPath(
		context.Background(),
		&remoteruntime.GitContext{Client: client},
		"obj-1",
		dstPath,
		&drsapi.DrsObject{Id: "obj-1", Size: int64(len(payload))},
		&drsapi.AccessURL{Url: "https://signed.example/object.bin"},
		sydownload.DownloadOptions{
			MultipartThreshold: int64(len(payload) + 1),
			Concurrency:        1,
			ChunkSize:          int64(len(payload)),
			RetryStrategy:      immediateDownloadRetry{},
		},
	)
	if err != nil {
		t.Fatalf("DownloadResolvedToPath returned error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("expected one retry, got %d requests", requests)
	}
	contents, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(contents) != payload {
		t.Fatalf("unexpected downloaded payload %q", contents)
	}

	var _ sytransfer.RetryStrategy = immediateDownloadRetry{}
}

func TestDownloadResolvedToPathRefreshesExpiredAccessURL(t *testing.T) {
	t.Parallel()

	const chunkSize = int64(1024 * 1024)
	firstChunk := bytes.Repeat([]byte("a"), int(chunkSize))
	secondChunk := bytes.Repeat([]byte("b"), int(chunkSize))
	thirdChunk := bytes.Repeat([]byte("c"), int(chunkSize))
	want := append(append(append([]byte(nil), firstChunk...), secondChunk...), thirdChunk...)
	var expiredRequests atomic.Int32
	var refreshRequests atomic.Int32
	expiredReady := make(chan struct{})

	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := func(status int, body []byte) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		}

		switch {
		case r.URL.Path == "/download/expired" && r.Header.Get("Range") == "bytes=0-1048575":
			return response(http.StatusPartialContent, firstChunk)
		case r.URL.Path == "/download/expired":
			if expiredRequests.Add(1) == 2 {
				close(expiredReady)
			}
			<-expiredReady
			return response(http.StatusForbidden, []byte("AccessDenied: signed URL expired"))
		case r.URL.Path == "/ga4gh/drs/v1/objects/obj-1/access/s3":
			refreshRequests.Add(1)
			return response(http.StatusOK, []byte(`{"url":"https://signed.example/download/fresh"}`))
		case r.URL.Path == "/download/fresh" && r.Header.Get("Range") == "bytes=1048576-2097151":
			return response(http.StatusPartialContent, secondChunk)
		case r.URL.Path == "/download/fresh" && r.Header.Get("Range") == "bytes=2097152-3145727":
			return response(http.StatusPartialContent, thirdChunk)
		default:
			return nil, io.EOF
		}
	})}

	client, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	accessID := "s3"
	methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeS3, AccessId: &accessID}}
	dstPath := filepath.Join(t.TempDir(), "object.bin")
	err = DownloadResolvedToPath(
		context.Background(),
		&remoteruntime.GitContext{Client: client},
		"obj-1",
		dstPath,
		&drsapi.DrsObject{Id: "obj-1", Size: int64(len(want)), AccessMethods: &methods},
		&drsapi.AccessURL{Url: "https://signed.example/download/expired"},
		sydownload.DownloadOptions{
			MultipartThreshold: 1,
			Concurrency:        2,
			ChunkSize:          chunkSize,
			RetryStrategy:      immediateDownloadRetry{},
		},
	)
	if err != nil {
		t.Fatalf("DownloadResolvedToPath returned error: %v", err)
	}
	if refreshRequests.Load() != 1 {
		t.Fatalf("access URL refresh requests = %d, want 1", refreshRequests.Load())
	}
	got, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("downloaded payload does not match the ranged responses")
	}
}
