package transfer

import (
	"io"
	"strings"
	"testing"

	sycommon "github.com/calypr/syfon/client/common"
)

func TestStreamingProgressReportsBeforeBodyClosesAndDeduplicatesRetries(t *testing.T) {
	var events []sycommon.ProgressEvent
	source := newStreamingProgressSource(nil, func(event sycommon.ProgressEvent) error {
		events = append(events, event)
		return nil
	}, "large.bin", 12, 0)

	first := &progressBody{ReadCloser: io.NopCloser(strings.NewReader("abcdefgh")), source: source}
	buf := make([]byte, 4)
	if _, err := first.Read(buf); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].BytesSoFar != 4 {
		t.Fatalf("progress before body closes = %+v, want 4 bytes", events)
	}
	if _, err := first.Read(buf); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1].BytesSoFar; got != 8 {
		t.Fatalf("progress after first body = %d, want 8", got)
	}

	retry := &progressBody{ReadCloser: io.NopCloser(strings.NewReader("efghijkl")), source: source, offset: 4}
	if _, err := io.Copy(io.Discard, retry); err != nil {
		t.Fatal(err)
	}
	if err := retry.Close(); err != nil {
		t.Fatal(err)
	}
	if got := events[len(events)-1].BytesSoFar; got != 12 {
		t.Fatalf("retry progress = %d, want 12 unique bytes", got)
	}
}
