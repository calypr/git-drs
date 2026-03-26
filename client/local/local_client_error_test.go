package local

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	registerErr error
	object      *drs.DRSObject
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
	if f.registerErr != nil {
		return nil, f.registerErr
	}
	if record == nil {
		return nil, errors.New("nil record")
	}
	return record, nil
}
func (f *fakeMetadata) RegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
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
	if !strings.Contains(err.Error(), "upload failed for") || !strings.Contains(err.Error(), "register fail from server") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewLocalClientStillBuildsDeps(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	c := NewLocalClient(LocalRemote{BaseURL: "http://example.invalid", ProjectID: "p", Bucket: "b"}, logger)
	if c == nil || c.meta == nil || c.uploads == nil || c.downloads == nil {
		t.Fatalf("expected LocalClient dependencies to be initialized")
	}
}
