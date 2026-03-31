package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/download"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/lfs"
)

type fakeMetadata struct {
	registerErr          error
	object               *drs.DRSObject
	registerRecordCalls  int
	registerRecordsCalls int
}

func (f *fakeMetadata) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	if f.object != nil {
		return f.object, nil
	}
	name := "obj.bin"
	return &drs.DRSObject{Id: id, Name: &name, Size: 1}, nil
}
func (f *fakeMetadata) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	ch := make(chan drs.DRSObjectResult)
	close(ch)
	return ch, nil
}
func (f *fakeMetadata) ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error) {
	ch := make(chan drs.DRSObjectResult)
	close(ch)
	return ch, nil
}
func (f *fakeMetadata) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	return []drs.DRSObject{}, nil
}
func (f *fakeMetadata) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return map[string][]drs.DRSObject{}, nil
}
func (f *fakeMetadata) DeleteRecordsByProject(ctx context.Context, projectId string) error {
	return nil
}
func (f *fakeMetadata) DeleteRecord(ctx context.Context, did string) error { return nil }
func (f *fakeMetadata) GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error) {
	return []drs.DRSObject{}, nil
}
func (f *fakeMetadata) RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error) {
	f.registerRecordCalls++
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	if record == nil {
		return nil, errors.New("nil record")
	}
	return record, nil
}
func (f *fakeMetadata) RegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	f.registerRecordsCalls++
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	return records, nil
}
func (f *fakeMetadata) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return nil, errors.New("not implemented")
}

type fakeUploader struct {
	err   error
	calls int
}

func (f *fakeUploader) Upload(ctx context.Context, req common.FileUploadRequestObject) error {
	f.calls++
	return f.err
}

func (f *fakeUploader) ResolveUploadURLs(ctx context.Context, requests []common.UploadURLResolveRequest) ([]common.UploadURLResolveResponse, error) {
	results := make([]common.UploadURLResolveResponse, 0, len(requests))
	for _, req := range requests {
		results = append(results, common.UploadURLResolveResponse{
			GUID:     req.GUID,
			Filename: req.Filename,
			Bucket:   req.Bucket,
			Status:   http.StatusBadGateway,
			Error:    "not resolved in fake uploader",
		})
	}
	return results, nil
}

type fakeDownloader struct {
	resolveErr  error
	downloadErr error
	payload     []byte
}

func (f *fakeDownloader) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return "https://example.invalid/download", nil
}

func (f *fakeDownloader) DownloadToPath(ctx context.Context, guid string, dstPath string, opts download.DownloadOptions) error {
	if f.downloadErr != nil {
		return f.downloadErr
	}
	return os.WriteFile(dstPath, f.payload, 0644)
}

func newTestLocalClient(remote LocalRemote, meta metadataStore, up uploadService, down downloadService) *LocalClient {
	return &LocalClient{
		Remote:    remote,
		Logger:    logs.NewSlogNoOpLogger(),
		meta:      meta,
		uploads:   up,
		downloads: down,
		Config:    &LocalConfig{MultiPartThreshold: int64(5 * common.MB)},
	}
}

func writeTempFile(t *testing.T, dir string, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestLocalClient_RegisterFileErrorsWhenFileMissing(t *testing.T) {
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: "bucket-a"},
		&fakeMetadata{},
		&fakeUploader{},
		&fakeDownloader{},
	)
	_, err := client.RegisterFile(context.Background(), strings.Repeat("a", 64), "/tmp/definitely-missing-file")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "error reading local record") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalClient_RegisterFilePropagatesRegisterError(t *testing.T) {
	tmp := t.TempDir()
	path := writeTempFile(t, tmp, "x.bin", []byte("payload"))
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: "bucket-a"},
		&fakeMetadata{registerErr: errors.New("register failed")},
		&fakeUploader{},
		&fakeDownloader{},
	)

	_, err := client.RegisterFile(context.Background(), strings.Repeat("b", 64), path)
	if err == nil || !strings.Contains(err.Error(), "register failed") {
		t.Fatalf("expected register failure, got %v", err)
	}
}

func TestLocalClient_RegisterFileErrorsWhenBucketMissing(t *testing.T) {
	tmp := t.TempDir()
	path := writeTempFile(t, tmp, "x.bin", []byte("payload"))
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: ""},
		&fakeMetadata{},
		&fakeUploader{},
		&fakeDownloader{},
	)

	_, err := client.RegisterFile(context.Background(), strings.Repeat("c", 64), path)
	if err == nil || !strings.Contains(err.Error(), "bucket name is empty") {
		t.Fatalf("expected missing-bucket error, got %v", err)
	}
}

func TestLocalClient_RegisterFilePropagatesUploadError(t *testing.T) {
	tmp := t.TempDir()
	path := writeTempFile(t, tmp, "x.bin", []byte("payload"))
	up := &fakeUploader{err: errors.New("cannot sign upload")}
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: "bucket-a"},
		&fakeMetadata{},
		up,
		&fakeDownloader{},
	)

	_, err := client.RegisterFile(context.Background(), strings.Repeat("d", 64), path)
	if err == nil || !strings.Contains(err.Error(), "cannot sign upload") {
		t.Fatalf("expected upload error, got %v", err)
	}
	if up.calls != 1 {
		t.Fatalf("expected one upload call, got %d", up.calls)
	}
}

func TestLocalClient_GetDownloadURLResolveError(t *testing.T) {
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj"},
		&fakeMetadata{},
		&fakeUploader{},
		&fakeDownloader{resolveErr: errors.New("cannot resolve download URL")},
	)
	_, err := client.GetDownloadURL(context.Background(), "did-1")
	if err == nil || !strings.Contains(err.Error(), "cannot resolve download URL") {
		t.Fatalf("expected resolve error, got %v", err)
	}
}

func TestLocalClient_DownloadFileDownloadError(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out.bin")
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj"},
		&fakeMetadata{},
		&fakeUploader{},
		&fakeDownloader{downloadErr: errors.New("download exploded")},
	)

	err := client.DownloadFile(context.Background(), "did-1", dst)
	if err == nil || !strings.Contains(err.Error(), "download exploded") {
		t.Fatalf("expected download error, got %v", err)
	}
}

func TestLocalClient_DownloadFileWritesData(t *testing.T) {
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "out.bin")
	payload := []byte("hello-local-download")
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj"},
		&fakeMetadata{},
		&fakeUploader{},
		&fakeDownloader{payload: payload},
	)

	if err := client.DownloadFile(context.Background(), "did-1", dst); err != nil {
		t.Fatalf("unexpected download error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("unexpected payload: got %q want %q", string(got), string(payload))
	}
}

func TestLocalClient_BatchSyncForPushReturnsFileContextOnError(t *testing.T) {
	tmp := t.TempDir()
	path := writeTempFile(t, tmp, "x.bin", []byte("payload"))
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: "bucket-a"},
		&fakeMetadata{registerErr: errors.New("register fail from server")},
		&fakeUploader{},
		&fakeDownloader{},
	)

	files := map[string]lfs.LfsFileInfo{
		"oid1": {Name: path, Oid: strings.Repeat("e", 64)},
	}
	err := client.BatchSyncForPush(context.Background(), files)
	if err == nil {
		t.Fatal("expected batch sync error")
	}
	if !strings.Contains(err.Error(), "batch register failed") || !strings.Contains(err.Error(), "register fail from server") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalClient_BatchSyncForPushUsesBatchRegistration(t *testing.T) {
	tmp := t.TempDir()
	p1 := writeTempFile(t, tmp, "a.bin", []byte("payload-a"))
	p2 := writeTempFile(t, tmp, "b.bin", []byte("payload-b"))

	meta := &fakeMetadata{}
	up := &fakeUploader{}
	client := newTestLocalClient(
		LocalRemote{ProjectID: "proj", Bucket: "bucket-a"},
		meta,
		up,
		&fakeDownloader{},
	)

	files := map[string]lfs.LfsFileInfo{
		"oid1": {Name: p1, Oid: strings.Repeat("a", 64)},
		"oid2": {Name: p2, Oid: strings.Repeat("b", 64)},
	}
	if err := client.BatchSyncForPush(context.Background(), files); err != nil {
		t.Fatalf("unexpected batch sync error: %v", err)
	}
	if meta.registerRecordsCalls != 1 {
		t.Fatalf("expected one RegisterRecords call, got %d", meta.registerRecordsCalls)
	}
	if meta.registerRecordCalls != 0 {
		t.Fatalf("expected zero RegisterRecord calls, got %d", meta.registerRecordCalls)
	}
	if up.calls != 2 {
		t.Fatalf("expected two uploads, got %d", up.calls)
	}
}

func TestNewLocalClientStillBuildsDeps(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewLocalClient(LocalRemote{BaseURL: "http://example.invalid", ProjectID: "p", Bucket: "b"}, logger)
	if c == nil || c.meta == nil || c.uploads == nil || c.downloads == nil {
		t.Fatalf("expected LocalClient dependencies to be initialized")
	}
}

func TestNewLocalClient_RegisterRecordSendsBasicAuth(t *testing.T) {
	const username = "alice"
	const password = "secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index" {
			http.NotFound(w, r)
			return
		}
		u, p, ok := r.BasicAuth()
		if !ok || u != username || p != password {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("missing/invalid basic auth"))
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewLocalClient(LocalRemote{
		BaseURL:       srv.URL,
		ProjectID:     "p",
		Bucket:        "b",
		BasicUsername: username,
		BasicPassword: password,
	}, logger)

	name := "obj.bin"
	_, err := client.RegisterRecord(context.Background(), &drs.DRSObject{
		Id:   "did-1",
		Name: &name,
		Size: 1,
		Checksums: []drs.Checksum{
			{Type: "sha256", Checksum: strings.Repeat("a", 64)},
		},
	})
	if err != nil {
		t.Fatalf("expected register to succeed with basic auth, got: %v", err)
	}
}

func TestNewLocalClient_RegisterRecordUnauthorizedDoesNotAttemptFenceRefresh(t *testing.T) {
	const username = "alice"
	const password = "wrong"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/index" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewLocalClient(LocalRemote{
		BaseURL:       srv.URL,
		ProjectID:     "p",
		Bucket:        "b",
		BasicUsername: username,
		BasicPassword: password,
	}, logger)

	name := "obj.bin"
	_, err := client.RegisterRecord(context.Background(), &drs.DRSObject{
		Id:   "did-1",
		Name: &name,
		Size: 1,
		Checksums: []drs.Checksum{
			{Type: "sha256", Checksum: strings.Repeat("a", 64)},
		},
	})
	if err == nil {
		t.Fatalf("expected unauthorized error")
	}
	if strings.Contains(err.Error(), "APIKey is required to refresh access token") {
		t.Fatalf("unexpected fence refresh path for local auth: %v", err)
	}
}

func TestLocalClient_GetDownloadURLFallsBackFromS3AccessURL(t *testing.T) {
	oid := strings.Repeat("a", 64)
	did := "did-local-1"
	accessPath := "/ga4gh/drs/v1/objects/" + did + "/access/s3"
	var accessCalls int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/index":
			if got := r.URL.Query().Get("hash"); got != "sha256:"+oid {
				http.Error(w, "bad hash query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"records":[{"did":"%s","file_name":"x.bin","size":1,"hashes":{"sha256":"%s"},"urls":["s3://cbds/local-key"]}]}`, did, oid)
			return
		case r.Method == http.MethodGet && r.URL.Path == accessPath:
			accessCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"url":"https://example.invalid/signed-download"}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewLocalClient(LocalRemote{
		BaseURL:   srv.URL,
		ProjectID: "p",
		Bucket:    "b",
	}, logger)

	u, err := client.GetDownloadURL(context.Background(), oid)
	if err != nil {
		t.Fatalf("expected fallback URL resolution to succeed, got: %v", err)
	}
	if u == nil || u.Url != "https://example.invalid/signed-download" {
		t.Fatalf("unexpected download URL: %#v", u)
	}
	if accessCalls != 1 {
		t.Fatalf("expected one access fallback call, got %d", accessCalls)
	}
}

func TestLocalClient_GetDownloadURLUsesDirectHTTPURLWhenPresent(t *testing.T) {
	oid := strings.Repeat("b", 64)
	did := "did-local-2"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/index":
			if got := r.URL.Query().Get("hash"); got != "sha256:"+oid {
				http.Error(w, "bad hash query", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"records":[{"did":"%s","file_name":"x.bin","size":1,"hashes":{"sha256":"%s"},"urls":["https://example.invalid/direct-download"]}]}`, did, oid)
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer srv.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewLocalClient(LocalRemote{
		BaseURL:   srv.URL,
		ProjectID: "p",
		Bucket:    "b",
	}, logger)

	u, err := client.GetDownloadURL(context.Background(), oid)
	if err != nil {
		t.Fatalf("expected direct HTTP URL to be usable, got: %v", err)
	}
	if u == nil || u.Url != "https://example.invalid/direct-download" {
		t.Fatalf("unexpected download URL: %#v", u)
	}
}
