package transfer

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPullProgressRendererTTY(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(true)
	r.base.SetClock(func() time.Time { return time.Unix(0, 0) })

	files := []PullFile{
		{Name: "a.bin", Oid: "oid-1", Size: 100},
		{Name: "b.bin", Oid: "oid-2", Size: 100},
	}
	r.OnPlan(files)
	r.OnDownloadStart(files[0])
	r.OnDownloadProgress("a.bin", 50, 100)
	r.OnCheckoutStart(files[0])
	r.OnCompleted(files[0])
	r.OnCompleted(files[1])

	got := out.String()
	if !strings.Contains(got, "a.bin [============") {
		t.Fatalf("expected progress bar output for a.bin, got %q", got)
	}
}

func TestPullProgressRendererNonTTYThrottles(t *testing.T) {
	var out bytes.Buffer
	now := time.Unix(0, 0)
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(false)
	r.base.SetClock(func() time.Time { return now })

	file := PullFile{Name: "a.bin", Oid: "oid-1", Size: 100}
	r.OnPlan([]PullFile{file})
	initial := out.String()
	if !strings.Contains(initial, "a.bin") {
		t.Fatalf("expected initial non-tty progress line, got %q", initial)
	}

	r.OnDownloadStart(file)
	r.OnDownloadProgress("a.bin", 10, 100)
	if got := out.String(); got != initial {
		t.Fatalf("expected throttled output before interval, got %q", got)
	}

	now = now.Add(NonTTYProgressInterval)
	r.OnCompleted(file)
	got := out.String()
	if !strings.Contains(got, "100.0% 100 B/100 B") {
		t.Fatalf("expected rendered completion after interval, got %q", got)
	}
}
