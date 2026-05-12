package pushsync

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	localcommon "github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/drslog"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
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
