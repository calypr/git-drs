package pushsync

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	localcommon "github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	sycommon "github.com/calypr/syfon/client/common"
	"github.com/calypr/syfon/client/transfer"
)

type chunkedUploadBackend struct {
	uploaded int64
}

func (b *chunkedUploadBackend) Name() string { return "chunked-upload-backend" }

func (b *chunkedUploadBackend) Logger() transfer.TransferLogger { return transfer.NoOpLogger{} }

func (b *chunkedUploadBackend) Upload(_ context.Context, _ string, body io.Reader, _ int64) error {
	buf := make([]byte, 64*1024)
	for {
		n, err := body.Read(buf)
		b.uploaded += int64(n)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (b *chunkedUploadBackend) ResolveUploadURL(context.Context, string, string, sycommon.FileMetadata, string) (string, error) {
	return "https://upload.example/object", nil
}

func (b *chunkedUploadBackend) MultipartInit(context.Context, string) (string, error) {
	return "upload-id", nil
}

func (b *chunkedUploadBackend) MultipartPart(context.Context, string, string, int, io.Reader) (string, error) {
	return "etag", nil
}

func (b *chunkedUploadBackend) MultipartComplete(context.Context, string, string, []transfer.MultipartPart) error {
	return nil
}

func TestResolveUploadSourcePath_NoSentinelObjectForPointer(t *testing.T) {
	repo := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	worktreePath := filepath.Join(repo, "data.bin")
	if err := os.WriteFile(worktreePath, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:"+oid+"\nsize 1\n"), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}

	// Ensure the implicit cache root matches command behavior under the cwd.
	if _, err := os.Stat(localcommon.LFS_OBJS_PATH); err == nil {
		t.Fatalf("expected no local object cache at %s", localcommon.LFS_OBJS_PATH)
	}

	src, ok, err := resolveUploadSourcePath(oid, worktreePath, true)
	if err != nil {
		t.Fatalf("resolveUploadSourcePath: %v", err)
	}
	if ok || src != "" {
		t.Fatalf("expected pointer without local payload to skip upload source, got src=%q ok=%v", src, ok)
	}
}

func TestUploadFileForObjectSinglePartStreamsProgress(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "large.bin")
	payload := make([]byte, sycommon.OnProgressThreshold+257)
	for i := range payload {
		payload[i] = 'a'
	}
	if err := os.WriteFile(filePath, payload, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	rt := &pushRuntime{
		Logger: drslog.NewNoOpLogger(),
		Scope: pushScope{
			Organization: "syfon",
			Project:      "e2e",
			Bucket:       "syfon-e2e-bucket",
		},
		Tuning: pushTuning{MultiPartThreshold: int64(len(payload) + 1024)},
	}

	obj := &drsapi.DrsObject{
		Id:   "object-did",
		Size: int64(len(payload)),
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
	}

	backend := &chunkedUploadBackend{}
	oldBackend := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) transfer.MultipartBackend { return backend }
	t.Cleanup(func() { uploadBackendForRuntime = oldBackend })

	var events []sycommon.ProgressEvent
	progressEventsSeenDuringUpload := 0
	ctx := sycommon.WithOid(context.Background(), obj.Checksums[0].Checksum)
	ctx = sycommon.WithProgress(ctx, func(ev sycommon.ProgressEvent) error {
		events = append(events, ev)
		return nil
	})

	if err := uploadFileForObject(rt, ctx, obj, filePath, false); err != nil {
		t.Fatalf("uploadFileForObject returned error: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected streamed progress events, got %+v", events)
	}

	for _, ev := range events {
		if ev.Event == "progress" {
			progressEventsSeenDuringUpload++
		}
	}
	if progressEventsSeenDuringUpload < 2 {
		t.Fatalf("expected threshold and finalize progress events, got %+v", events)
	}
	last := events[len(events)-1]
	if last.BytesSoFar != int64(len(payload)) {
		t.Fatalf("final progress bytes = %d, want %d", last.BytesSoFar, len(payload))
	}
	if backend.uploaded != int64(len(payload)) {
		t.Fatalf("uploaded size = %d, want %d", backend.uploaded, len(payload))
	}
}

func TestUploadFileForObjectSinglePartUsesScopedUploadURLResolution(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "project-subpath.bin")
	if err := os.WriteFile(filePath, []byte("project subpath payload"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	oid := "412f8568bfb0e62937ee40c6fcdeaa1cf55910c558c0152250340356c8829a47"
	var uploadQuery map[string]string

	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/data/upload/f781273b-52eb-5ac2-a484-775235eef303":
			uploadQuery = map[string]string{
				"organization": r.URL.Query().Get("organization"),
				"project":      r.URL.Query().Get("project"),
				"file_name":    r.URL.Query().Get("file_name"),
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"url":"https://upload.example/scoped"}`)),
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

	rt := &pushRuntime{
		API: &config.GitContext{
			Client:       client,
			Organization: "syfon",
			ProjectId:    "e2e",
			BucketName:   "syfon-e2e-bucket",
			Logger:       drslog.NewNoOpLogger(),
		},
		Logger: drslog.NewNoOpLogger(),
		Scope: pushScope{
			Organization: "syfon",
			Project:      "e2e",
			Bucket:       "syfon-e2e-bucket",
			StoragePref:  "program-root/project-subpath",
		},
		Tuning: pushTuning{MultiPartThreshold: 1024},
	}

	obj := &drsapi.DrsObject{
		Id:   "f781273b-52eb-5ac2-a484-775235eef303",
		Size: 23,
		Checksums: []drsapi.Checksum{{
			Type:     "sha256",
			Checksum: oid,
		}},
	}

	backend := &pushUploadBackendStub{}
	oldBackend := uploadBackendForRuntime
	uploadBackendForRuntime = func(*pushRuntime) transfer.MultipartBackend { return backend }
	t.Cleanup(func() { uploadBackendForRuntime = oldBackend })

	if err := uploadFileForObject(rt, context.Background(), obj, filePath, false); err != nil {
		t.Fatalf("uploadFileForObject returned error: %v", err)
	}
	if uploadQuery["organization"] != "syfon" || uploadQuery["project"] != "e2e" {
		t.Fatalf("scoped upload query = %+v, want syfon/e2e", uploadQuery)
	}
	wantKey := "program-root/project-subpath/" + oid
	if uploadQuery["file_name"] != wantKey {
		t.Fatalf("scoped upload file_name = %q, want %q", uploadQuery["file_name"], wantKey)
	}
	if backend.lastUpload.url != "https://upload.example/scoped" {
		t.Fatalf("upload URL = %q, want scoped upload URL", backend.lastUpload.url)
	}
}
