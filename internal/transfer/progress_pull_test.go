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
	if !strings.Contains(initial, "a.bin: Checking local object cache") || strings.Contains(initial, "[") {
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
	if !strings.Contains(got, "a.bin: complete") {
		t.Fatalf("expected rendered completion after interval, got %q", got)
	}
}

func TestPullProgressRendererFinishesOnceAfterIndexRefresh(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(false)
	file := PullFile{Name: "large.bin", Size: 100}
	r.OnPlan([]PullFile{file})
	r.OnCheckoutStart(file)
	r.OnCheckoutProgress(file.Name, file.Size)
	r.OnIndexRefreshStart()
	r.OnCompleted(file)
	if err := r.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.String(), "large.bin: complete"); got != 1 {
		t.Fatalf("completion rendered %d times: %q", got, out.String())
	}
}

func TestPullProgressRendererShowsExistingFileVerification(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(true)
	file := PullFile{Name: "large.bin", Size: 100}
	r.OnPlan([]PullFile{file})
	r.OnExistingFileVerificationStart(file)
	r.OnExistingFileProgress(file.Name, 50)
	if got := out.String(); !strings.Contains(got, "50.0% 50 B/100 B checking existing file") {
		t.Fatalf("existing-file verification progress missing: %q", got)
	}
}

func TestPullProgressRendererShowsWorkAfterBytesArrive(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(true)
	file := PullFile{Name: "large.bin", Oid: "oid-1", Size: 100}
	r.OnPlan([]PullFile{file})
	r.OnDownloadStart(file)

	out.Reset()
	r.OnDownloadProgress(file.Name, file.Size, file.Size)
	if got := out.String(); !strings.Contains(got, "Downloaded; waiting for verification") || strings.Contains(got, "verifying download") || strings.Contains(got, " [") {
		t.Fatalf("full download must show queued verification without an active bar, got %q", got)
	}
	out.Reset()
	r.OnVerificationProgress(file.Name, 0)
	if got := out.String(); !strings.Contains(got, "0 B/100 B verifying download") || !strings.Contains(got, " [") {
		t.Fatalf("verification must show its bar when reading starts, got %q", got)
	}
	out.Reset()
	r.OnVerificationProgress(file.Name, 50)
	if got := out.String(); !strings.Contains(got, "50.0% 50 B/100 B verifying download") {
		t.Fatalf("verification bar must report bytes read, got %q", got)
	}

	out.Reset()
	r.OnCheckoutStart(file)
	if got := out.String(); !strings.Contains(got, "0 B/100 B checking out file") || !strings.Contains(got, " [") {
		t.Fatalf("checkout must start a separate copy bar, got %q", got)
	}
	out.Reset()
	r.OnCheckoutProgress(file.Name, 50)
	if got := out.String(); !strings.Contains(got, "50.0% 50 B/100 B checking out file") {
		t.Fatalf("checkout bar must report bytes copied, got %q", got)
	}
	out.Reset()
	r.OnIndexRefreshStart()
	if got := out.String(); !strings.Contains(got, "Refreshing Git index; rereading checked-out file") || strings.Contains(got, "complete") {
		t.Fatalf("index refresh must remain visible before completion, got %q", got)
	}

	out.Reset()
	r.OnCompleted(file)
	if got := out.String(); !strings.Contains(got, "complete") {
		t.Fatalf("completed pull must be distinguishable from in-progress work, got %q", got)
	}
}

func TestPullProgressRendererHeartbeatDuringZeroByteWait(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(true)
	r.heartbeatInterval = 10 * time.Millisecond
	file := PullFile{Name: "large.bin", Size: 100}
	r.OnPlan([]PullFile{file})
	r.OnDownloadStart(file)
	r.OnTransferStart(file.Name)
	r.StartHeartbeat()
	time.Sleep(35 * time.Millisecond)
	if err := r.Finish(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); strings.Count(got, "waiting for data") < 2 {
		t.Fatalf("expected timed zero-byte updates, got %q", got)
	}
}

func TestPullProgressRendererOnlyShowsBarAfterBytesArrive(t *testing.T) {
	var out bytes.Buffer
	r := NewPullProgressRenderer(&out)
	r.base.SetTTY(true)
	file := PullFile{Name: "large.bin", Size: 100}
	r.OnPlan([]PullFile{file})
	r.OnStage("Looking up DRS records")
	r.OnDownloadStart(file)
	r.OnConnectionStart(file.Name)
	r.OnTransferStart(file.Name)
	if got := out.String(); strings.Contains(got, " [") || !strings.Contains(got, "Looking up DRS records") || !strings.Contains(got, "Opening download connection") || !strings.Contains(got, "Connected; waiting for data") {
		t.Fatalf("expected precise status without a bar before data, got %q", got)
	}
	out.Reset()
	r.OnDownloadProgress(file.Name, 10, 100)
	if got := out.String(); !strings.Contains(got, "[==") || !strings.Contains(got, "10 B/100 B") {
		t.Fatalf("expected bar once bytes arrive, got %q", got)
	}
}
