package transfer

import (
	"bufio"
	"bytes"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/git-drs/lfs"
)

func TestProgressReporterReportEmitsJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := encoder.NewStreamEncoder(buf)
	writer := bufio.NewWriter(buf)

	progress := newProgressReporter("oid", 0, enc, writer)
	if err := progress.Report(128); err != nil {
		t.Fatalf("report error: %v", err)
	}

	var message lfs.ProgressResponse
	if err := sonic.ConfigFastest.Unmarshal(bytes.TrimSpace(buf.Bytes()), &message); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if message.Event != "progress" || message.Oid != "oid" {
		t.Fatalf("unexpected progress metadata: %+v", message)
	}
	if message.BytesSoFar != 128 || message.BytesSinceLast != 128 {
		t.Fatalf("unexpected progress bytes: %+v", message)
	}
}

func TestProgressReporterFinalizeForcesJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	enc := encoder.NewStreamEncoder(buf)
	writer := bufio.NewWriter(buf)

	progress := newProgressReporter("oid", 1000, enc, writer)
	progress.minInterval = time.Hour
	progress.minBytesToEmit = 1024 * 1024
	progress.lastEmit = time.Now()

	if err := progress.Report(100); err != nil {
		t.Fatalf("report error: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no progress output before finalize")
	}

	if err := progress.Finalize(); err != nil {
		t.Fatalf("finalize error: %v", err)
	}

	var message lfs.ProgressResponse
	if err := sonic.ConfigFastest.Unmarshal(bytes.TrimSpace(buf.Bytes()), &message); err != nil {
		t.Fatalf("unmarshal progress: %v", err)
	}
	if message.Event != "progress" || message.Oid != "oid" {
		t.Fatalf("unexpected progress metadata: %+v", message)
	}
	if message.BytesSoFar != 1000 || message.BytesSinceLast != 1000 {
		t.Fatalf("unexpected progress bytes: %+v", message)
	}
}
