package transfer

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestUploadProgressRendererTTY(t *testing.T) {
	var out bytes.Buffer
	r := NewUploadProgressRenderer(&out)
	r.base.SetTTY(true)

	r.OnUploadPlan(UploadPlanSummary{
		Files: []UploadPlanFile{
			{OID: "oid-1", Path: "a.bin", Bytes: 100},
			{OID: "oid-2", Path: "b.bin", Bytes: 100},
		},
		TotalFiles: 2,
		TotalBytes: 200,
	})
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 0, TotalBytes: 100, Phase: UploadProgressUploading})
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 50, BytesSinceLast: 50, TotalBytes: 100, Phase: UploadProgressUploading})
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: UploadProgressCompleted})
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-2", Path: "b.bin", BytesSoFar: 0, TotalBytes: 100, Phase: UploadProgressUploading})
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-2", Path: "b.bin", BytesSoFar: 100, TotalBytes: 100, Phase: UploadProgressCompleted})
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
}

func TestUploadProgressRendererNonTTYThrottles(t *testing.T) {
	var out bytes.Buffer
	r := NewUploadProgressRenderer(&out)
	r.base.SetTTY(false)
	now := time.Unix(0, 0)
	r.base.SetClock(func() time.Time { return now })

	r.OnUploadPlan(UploadPlanSummary{
		Files:      []UploadPlanFile{{OID: "oid-1", Path: "a.bin", Bytes: 100}},
		TotalFiles: 1,
		TotalBytes: 100,
	})
	first := out.String()
	if first == "" {
		t.Fatal("expected initial non-tty progress line")
	}

	r.OnUploadProgress(UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 10, BytesSinceLast: 10, TotalBytes: 100, Phase: UploadProgressUploading})
	if out.String() != first {
		t.Fatalf("expected throttled output to remain unchanged, got %q", out.String())
	}

	now = now.Add(3 * time.Second)
	r.OnUploadProgress(UploadProgressEvent{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: UploadProgressCompleted})
	got := out.String()
	if strings.Count(got, "\n") < 2 {
		t.Fatalf("expected throttled summary updates, got %q", got)
	}
}

func TestUploadProgressRendererMetadataOnly(t *testing.T) {
	var out bytes.Buffer
	r := NewUploadProgressRenderer(&out)
	r.base.SetTTY(true)

	r.OnMetadataPlan(MetadataPlanSummary{TotalObjects: 3})
	r.OnMetadataProgress(MetadataProgressEvent{Completed: 0, Total: 3, Phase: MetadataProgressRegistering})
	r.OnMetadataProgress(MetadataProgressEvent{Completed: 3, Total: 3, Phase: MetadataProgressCompleted})
	r.Finish()

	got := out.String()
	if !strings.Contains(got, "registering metadata") {
		t.Fatalf("expected metadata line, got %q", got)
	}
}

func TestUploadProgressRendererConcurrentProgress(t *testing.T) {
	var out bytes.Buffer
	r := NewUploadProgressRenderer(&out)
	r.base.SetTTY(false)

	r.OnUploadPlan(UploadPlanSummary{
		Files: []UploadPlanFile{
			{OID: "oid-1", Path: "a.bin", Bytes: 100},
			{OID: "oid-2", Path: "b.bin", Bytes: 100},
		},
		TotalFiles: 2,
		TotalBytes: 200,
	})

	events := []UploadProgressEvent{
		{OID: "oid-1", Path: "a.bin", BytesSoFar: 10, BytesSinceLast: 10, TotalBytes: 100, Phase: UploadProgressUploading},
		{OID: "oid-2", Path: "b.bin", BytesSoFar: 20, BytesSinceLast: 20, TotalBytes: 100, Phase: UploadProgressUploading},
		{OID: "oid-1", Path: "a.bin", BytesSoFar: 100, TotalBytes: 100, Phase: UploadProgressCompleted},
		{OID: "oid-2", Path: "b.bin", BytesSoFar: 100, TotalBytes: 100, Phase: UploadProgressCompleted},
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
