package transfer

import (
	"bufio"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/git-drs/lfs"
)

type progressReporter struct {
	oid            string
	size           int64
	encoder        *encoder.StreamEncoder
	output         *bufio.Writer
	lastEmit       time.Time
	lastReported   int64
	bytesSoFar     int64
	minInterval    time.Duration
	minBytesToEmit int64
	mu             sync.Mutex
}

func newProgressReporter(oid string, size int64, encoder *encoder.StreamEncoder, output *bufio.Writer) *progressReporter {
	return &progressReporter{
		oid:            oid,
		size:           size,
		encoder:        encoder,
		output:         output,
		minInterval:    200 * time.Millisecond,
		minBytesToEmit: 256 * 1024,
	}
}

func (p *progressReporter) Report(delta int64) error {
	if delta <= 0 {
		return nil
	}
	bytesSoFar := atomic.AddInt64(&p.bytesSoFar, delta)
	return p.maybeEmit(bytesSoFar, false)
}

func (p *progressReporter) Finalize() error {
	bytesSoFar := atomic.LoadInt64(&p.bytesSoFar)
	if p.size > 0 {
		bytesSoFar = p.size
		atomic.StoreInt64(&p.bytesSoFar, bytesSoFar)
	}
	return p.maybeEmit(bytesSoFar, true)
}

func (p *progressReporter) maybeEmit(bytesSoFar int64, force bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	bytesSinceLast := bytesSoFar - p.lastReported
	if bytesSinceLast <= 0 {
		return nil
	}

	if !force {
		if time.Since(p.lastEmit) < p.minInterval && bytesSinceLast < p.minBytesToEmit && (p.size <= 0 || bytesSoFar < p.size) {
			return nil
		}
	}

	lfs.WriteProgressMessage(p.encoder, p.oid, bytesSoFar, bytesSinceLast)
	if err := p.output.Flush(); err != nil {
		return err
	}
	p.lastReported = bytesSoFar
	p.lastEmit = time.Now()
	return nil
}
