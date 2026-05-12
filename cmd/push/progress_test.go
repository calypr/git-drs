package push

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/pushsync"
)

func TestUploadProgressRendererTTY(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.isTTY = true

	r.OnUploadPlan(pushsync.UploadPlanSummary{
		Files: []pushsync.UploadPlanFile{
			{OID: "oid-1", Path: "a.bin", Bytes: 100},
			{OID: "oid-2", Path: "b.bin", Bytes: 100},
		},
		TotalFiles: 2,
		TotalBytes: 200,
	})
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 0, TotalBytes: 100, Phase: pushsync.UploadProgressUploading})
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 50, BytesSinceLast: 50, TotalBytes: 100, Phase: pushsync.UploadProgressUploading})
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: pushsync.UploadProgressCompleted})
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-2", Path: "b.bin", BytesSoFar: 0, TotalBytes: 100, Phase: pushsync.UploadProgressUploading})
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-2", Path: "b.bin", BytesSoFar: 100, TotalBytes: 100, Phase: pushsync.UploadProgressCompleted})
	r.Finish()

	got := out.String()
	if !strings.Contains(got, "1/2 a.bin (uploading)") {
		t.Fatalf("expected first file uploading line, got %q", got)
	}
	if !strings.Contains(got, "[ ] 2/2 b.bin (pending)") {
		t.Fatalf("expected second file pending line, got %q", got)
	}
	if !strings.Contains(got, "[*] 2/2 b.bin (complete)") {
		t.Fatalf("expected completed second file line, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}
}

func TestUploadProgressRendererNonTTYThrottles(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.isTTY = false
	now := time.Unix(0, 0)
	r.now = func() time.Time { return now }

	r.OnUploadPlan(pushsync.UploadPlanSummary{
		Files:      []pushsync.UploadPlanFile{{OID: "oid-1", Path: "a.bin", Bytes: 100}},
		TotalFiles: 1,
		TotalBytes: 100,
	})
	first := out.String()
	if first == "" {
		t.Fatal("expected initial non-tty progress line")
	}

	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 10, BytesSinceLast: 10, TotalBytes: 100, Phase: pushsync.UploadProgressUploading})
	if out.String() != first {
		t.Fatalf("expected throttled output to remain unchanged, got %q", out.String())
	}

	now = now.Add(3 * time.Second)
	r.OnUploadProgress(pushsync.UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: pushsync.UploadProgressCompleted})
	got := out.String()
	if strings.Count(got, "\n") < 2 {
		t.Fatalf("expected throttled summary updates, got %q", got)
	}
	if !strings.Contains(got, "[*] 1/1 a.bin (complete)") {
		t.Fatalf("expected non-tty progress summary, got %q", got)
	}
}
