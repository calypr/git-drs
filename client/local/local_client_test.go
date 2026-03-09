package local

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/calypr/data-client/common"
	drs "github.com/calypr/data-client/drs"
)

type mockBackend struct {
	uploadURLCalls         int
	uploadCalls            int
	initMultipartCalls     int
	partURLCalls           int
	uploadPartCalls        int
	completeMultipartCalls int
}

func (m *mockBackend) Name() string { return "mock" }
func (m *mockBackend) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
func (m *mockBackend) GetFileDetails(ctx context.Context, guid string) (*drs.DRSObject, error) {
	return &drs.DRSObject{Id: guid, Size: 1, Name: "x"}, nil
}
func (m *mockBackend) GetObjectByHash(ctx context.Context, checksumType, checksum string) ([]drs.DRSObject, error) {
	return nil, nil
}
func (m *mockBackend) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return map[string][]drs.DRSObject{}, nil
}
func (m *mockBackend) GetDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	return "https://example.invalid/download", nil
}
func (m *mockBackend) Download(ctx context.Context, fdr *common.FileDownloadResponseObject) (*http.Response, error) {
	return nil, nil
}
func (m *mockBackend) Register(ctx context.Context, obj *drs.DRSObject) (*drs.DRSObject, error) {
	return obj, nil
}
func (m *mockBackend) BatchRegister(ctx context.Context, objs []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return objs, nil
}
func (m *mockBackend) GetUploadURL(ctx context.Context, guid string, filename string, metadata common.FileMetadata, bucket string) (string, error) {
	m.uploadURLCalls++
	return "https://example.invalid/upload", nil
}
func (m *mockBackend) InitMultipartUpload(ctx context.Context, guid string, filename string, bucket string) (*common.MultipartUploadInit, error) {
	m.initMultipartCalls++
	return &common.MultipartUploadInit{GUID: guid, UploadID: "up-1"}, nil
}
func (m *mockBackend) GetMultipartUploadURL(ctx context.Context, key string, uploadID string, partNumber int32, bucket string) (string, error) {
	m.partURLCalls++
	return "https://example.invalid/part", nil
}
func (m *mockBackend) CompleteMultipartUpload(ctx context.Context, key string, uploadID string, parts []common.MultipartUploadPart, bucket string) error {
	m.completeMultipartCalls++
	return nil
}
func (m *mockBackend) Upload(ctx context.Context, url string, body io.Reader, size int64) error {
	m.uploadCalls++
	return nil
}
func (m *mockBackend) UploadPart(ctx context.Context, url string, body io.Reader, size int64) (string, error) {
	m.uploadPartCalls++
	return "etag-1", nil
}

func TestRegisterFileUsesSingleUploadPath(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/small.bin"
	if err := os.WriteFile(path, []byte("small"), 0o644); err != nil {
		t.Fatal(err)
	}

	mb := &mockBackend{}
	lc := &LocalClient{
		Remote: LocalRemote{
			BaseURL:      "http://localhost:8080",
			Bucket:       "bucket-a",
			ProjectID:    "p",
			Organization: "o",
		},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Backend: mb,
		Config:  &LocalConfig{MultiPartThreshold: 1024 * 1024},
	}

	oid := "1111111111111111111111111111111111111111111111111111111111111111"
	if _, err := lc.RegisterFile(context.Background(), oid, path); err != nil {
		t.Fatalf("RegisterFile failed: %v", err)
	}

	if mb.uploadURLCalls != 1 || mb.uploadCalls != 1 {
		t.Fatalf("expected single upload path, got uploadURLCalls=%d uploadCalls=%d", mb.uploadURLCalls, mb.uploadCalls)
	}
	if mb.initMultipartCalls != 0 || mb.uploadPartCalls != 0 || mb.completeMultipartCalls != 0 {
		t.Fatalf("did not expect multipart path, got init=%d part=%d complete=%d", mb.initMultipartCalls, mb.uploadPartCalls, mb.completeMultipartCalls)
	}
}

func TestRegisterFileUsesMultipartUploadPath(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/large.bin"
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Use sparse file >100MB so OptimalChunkSize yields multipart chunks < file size.
	if err := f.Truncate(120 * common.MB); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	mb := &mockBackend{}
	lc := &LocalClient{
		Remote: LocalRemote{
			BaseURL:      "http://localhost:8080",
			Bucket:       "bucket-a",
			ProjectID:    "p",
			Organization: "o",
		},
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Backend: mb,
		Config: &LocalConfig{
			MultiPartThreshold: 1,
			UploadConcurrency:  4,
		},
	}

	oid := "2222222222222222222222222222222222222222222222222222222222222222"
	if _, err := lc.RegisterFile(context.Background(), oid, path); err != nil {
		t.Fatalf("RegisterFile failed: %v", err)
	}

	if mb.initMultipartCalls == 0 || mb.partURLCalls == 0 || mb.uploadPartCalls == 0 || mb.completeMultipartCalls == 0 {
		t.Fatalf("expected multipart path, got init=%d partURL=%d uploadPart=%d complete=%d", mb.initMultipartCalls, mb.partURLCalls, mb.uploadPartCalls, mb.completeMultipartCalls)
	}
	if mb.uploadPartCalls <= 1 {
		t.Fatalf("expected multiple multipart part uploads, got %d", mb.uploadPartCalls)
	}
	if mb.uploadURLCalls != 0 || mb.uploadCalls != 0 {
		t.Fatalf("did not expect single upload path, got uploadURLCalls=%d uploadCalls=%d", mb.uploadURLCalls, mb.uploadCalls)
	}
}
