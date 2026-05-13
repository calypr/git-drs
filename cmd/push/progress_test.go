package push

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/pushsync"
)

func TestUploadProgressRendererTTY(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.base.SetTTY(true)

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
	if !strings.Contains(got, "a.bin [============            ]  50.0% 50 B/100 B") {
		t.Fatalf("expected first file uploading line, got %q", got)
	}
	if !strings.Contains(got, "b.bin [                        ]   0.0% 0 B/100 B") {
		t.Fatalf("expected second file pending line, got %q", got)
	}
	if !strings.Contains(got, "b.bin [========================] 100.0% 100 B/100 B") {
		t.Fatalf("expected completed second file line, got %q", got)
	}
	if strings.Contains(got, "(uploading)") || strings.Contains(got, "(pending)") || strings.Contains(got, "(complete)") {
		t.Fatalf("did not expect parenthesized state text, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("expected trailing newline, got %q", got)
	}
}

func TestUploadProgressRendererNonTTYThrottles(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.base.SetTTY(false)
	now := time.Unix(0, 0)
	r.base.SetClock(func() time.Time { return now })

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
	if !strings.Contains(got, "a.bin [========================] 100.0% 100 B/100 B") {
		t.Fatalf("expected non-tty progress summary, got %q", got)
	}
	if strings.Contains(got, "1/1") || strings.Contains(got, "[*]") {
		t.Fatalf("did not expect positional or completion prefix clutter, got %q", got)
	}
}

func TestUploadProgressRendererDoesNotShowFullCompletionBeforeCompleteEvent(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.base.SetTTY(true)

	total := int64(500 * 1024 * 1024)
	r.OnUploadPlan(pushsync.UploadPlanSummary{
		Files:      []pushsync.UploadPlanFile{{OID: "oid-1", Path: "large.bin", Bytes: total}},
		TotalFiles: 1,
		TotalBytes: total,
	})
	r.OnUploadProgress(pushsync.UploadProgressEvent{
		OID:        "oid-1",
		Path:       "large.bin",
		BytesSoFar: total,
		TotalBytes: total,
		Phase:      pushsync.UploadProgressUploading,
	})

	got := out.String()
	if !strings.Contains(got, "99.9%") {
		t.Fatalf("expected in-flight upload to stay below 100%%, got %q", got)
	}
	if !strings.Contains(got, "<500.0 MiB/500.0 MiB") {
		t.Fatalf("expected in-flight byte label to avoid full equality, got %q", got)
	}
	if strings.Contains(got, "100.0%") {
		t.Fatalf("did not expect in-flight upload to render as 100%%, got %q", got)
	}
}

func TestUploadProgressRendererHadUploads(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	if r.HadUploads() {
		t.Fatal("expected fresh renderer to report no uploads")
	}

	r.OnUploadPlan(pushsync.UploadPlanSummary{
		Files:      []pushsync.UploadPlanFile{{OID: "oid-1", Path: "a.bin", Bytes: 1}},
		TotalFiles: 1,
		TotalBytes: 1,
	})
	if !r.HadUploads() {
		t.Fatal("expected renderer to report uploads after a non-empty plan")
	}

	r.Finish()
	if r.HadUploads() {
		t.Fatal("expected renderer to reset after finish")
	}
}

func TestUploadProgressRendererConcurrentProgress(t *testing.T) {
	var out bytes.Buffer
	r := newUploadProgressRenderer(&out)
	r.base.SetTTY(false)

	r.OnUploadPlan(pushsync.UploadPlanSummary{
		Files: []pushsync.UploadPlanFile{
			{OID: "oid-1", Path: "a.bin", Bytes: 100},
			{OID: "oid-2", Path: "b.bin", Bytes: 100},
		},
		TotalFiles: 2,
		TotalBytes: 200,
	})

	events := []pushsync.UploadProgressEvent{
		{OID: "oid-1", Path: "a.bin", BytesSoFar: 10, BytesSinceLast: 10, TotalBytes: 100, Phase: pushsync.UploadProgressUploading},
		{OID: "oid-2", Path: "b.bin", BytesSoFar: 20, BytesSinceLast: 20, TotalBytes: 100, Phase: pushsync.UploadProgressUploading},
		{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: pushsync.UploadProgressCompleted},
		{OID: "oid-2", Path: "b.bin", BytesSoFar: 100, TotalBytes: 100, Phase: pushsync.UploadProgressCompleted},
	}

	var wg sync.WaitGroup
	wg.Add(len(events))
	for _, ev := range events {
		ev := ev
		go func() {
			defer wg.Done()
			r.OnUploadProgress(ev)
		}()
	}
	wg.Wait()
	r.Finish()

	got := out.String()
	if !strings.Contains(got, "a.bin") || !strings.Contains(got, "b.bin") {
		t.Fatalf("expected both files in concurrent progress output, got %q", got)
	}
}
