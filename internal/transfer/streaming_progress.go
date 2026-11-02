package transfer

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	sycommon "github.com/calypr/syfon/client/common"
	sytransfer "github.com/calypr/syfon/client/transfer"
)

const streamingProgressInterval = 500 * time.Millisecond

type byteRange struct{ start, end int64 }

// streamingProgressSource reports bytes as response bodies are read. The syfon
// downloader buffers its own progress until a transfer finishes.
type streamingProgressSource struct {
	sytransfer.ReadBackend
	callback sycommon.ProgressCallback
	oid      string
	total    int64
	initial  int64
	mu       sync.Mutex
	ranges   []byteRange
	current  int64
	reported int64
	last     time.Time
}

func newStreamingProgressSource(source sytransfer.ReadBackend, callback sycommon.ProgressCallback, oid string, total, initial int64) *streamingProgressSource {
	return &streamingProgressSource{ReadBackend: source, callback: callback, oid: oid, total: total, initial: initial, reported: initial}
}

func (s *streamingProgressSource) GetReader(ctx context.Context, guid string) (io.ReadCloser, error) {
	body, err := s.ReadBackend.GetReader(ctx, guid)
	if err != nil {
		return nil, err
	}
	if err := s.callback(sycommon.ProgressEvent{Event: "transfer-start", Oid: s.oid}); err != nil {
		_ = body.Close()
		return nil, err
	}
	return &progressBody{ReadCloser: body, source: s}, nil
}

func (s *streamingProgressSource) GetRangeReader(ctx context.Context, guid string, offset, length int64) (io.ReadCloser, error) {
	body, err := s.ReadBackend.GetRangeReader(ctx, guid, offset, length)
	if err != nil {
		if errors.Is(err, sytransfer.ErrRangeIgnored) {
			s.mu.Lock()
			s.initial, s.current, s.reported = 0, 0, 0
			s.ranges = nil
			s.mu.Unlock()
			_ = s.callback(sycommon.ProgressEvent{Event: "transfer-restart", Oid: s.oid})
		}
		return nil, err
	}
	if err := s.callback(sycommon.ProgressEvent{Event: "transfer-start", Oid: s.oid}); err != nil {
		_ = body.Close()
		return nil, err
	}
	return &progressBody{ReadCloser: body, source: s, offset: offset}, nil
}

type progressBody struct {
	io.ReadCloser
	source *streamingProgressSource
	offset int64
}

func (b *progressBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		progressErr := b.source.add(b.offset, b.offset+int64(n), false)
		b.offset += int64(n)
		if progressErr != nil {
			return n, progressErr
		}
	}
	return n, err
}

func (b *progressBody) Close() error {
	progressErr := b.source.add(0, 0, true)
	closeErr := b.ReadCloser.Close()
	if progressErr != nil {
		return progressErr
	}
	return closeErr
}

func (s *streamingProgressSource) add(start, end int64, flush bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if end > start {
		insert := sort.Search(len(s.ranges), func(i int) bool { return s.ranges[i].end >= start })
		merged := byteRange{start: start, end: end}
		last := insert
		for last < len(s.ranges) && s.ranges[last].start <= merged.end {
			if s.ranges[last].start < merged.start {
				merged.start = s.ranges[last].start
			}
			if s.ranges[last].end > merged.end {
				merged.end = s.ranges[last].end
			}
			s.current -= s.ranges[last].end - s.ranges[last].start
			last++
		}
		s.ranges = append(s.ranges[:insert], append([]byteRange{merged}, s.ranges[last:]...)...)
		s.current += merged.end - merged.start
	}
	if s.initial+s.current == s.reported || (!flush && time.Since(s.last) < streamingProgressInterval) {
		return nil
	}
	current := s.initial + s.current
	if s.total > 0 && current > s.total {
		current = s.total
	}
	delta := current - s.reported
	if delta <= 0 {
		return nil
	}
	if err := s.callback(sycommon.ProgressEvent{Event: "progress", Oid: s.oid, BytesSoFar: current, BytesSinceLast: delta}); err != nil {
		return err
	}
	s.reported = current
	s.last = time.Now()
	return nil
}
