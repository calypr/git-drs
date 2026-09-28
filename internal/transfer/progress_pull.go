package transfer

import (
	"fmt"
	"io"
	"sync"
	"time"
)

const pullHeartbeatInterval = 2 * time.Second

type pullProgressPhase string

const (
	pullProgressPending     pullProgressPhase = "pending"
	pullProgressDownloading pullProgressPhase = "downloading"
	pullProgressVerifying   pullProgressPhase = "verifying"
	pullProgressCheckingOut pullProgressPhase = "checking_out"
	pullProgressCompleted   pullProgressPhase = "completed"
)

type pullFileProgress struct {
	path       string
	total      int64
	current    int64
	phase      pullProgressPhase
	phaseSince time.Time
	lastBytes  time.Time
}

type PullProgressRenderer struct {
	base              *Renderer
	err               error
	planned           bool
	files             map[string]*pullFileProgress
	fileOrder         []string
	mu                sync.Mutex
	stopHeartbeat     chan struct{}
	heartbeatDone     chan struct{}
	now               func() time.Time
	heartbeatInterval time.Duration
}

func NewPullProgressRenderer(out io.Writer) *PullProgressRenderer {
	return &PullProgressRenderer{
		base:              NewRenderer(out),
		files:             make(map[string]*pullFileProgress),
		now:               time.Now,
		heartbeatInterval: pullHeartbeatInterval,
	}
}

func (r *PullProgressRenderer) StartHeartbeat() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopHeartbeat != nil || !r.planned {
		return
	}
	r.stopHeartbeat = make(chan struct{})
	r.heartbeatDone = make(chan struct{})
	stop, done := r.stopHeartbeat, r.heartbeatDone
	interval := r.heartbeatInterval
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.mu.Lock()
				r.render(false)
				r.mu.Unlock()
			case <-stop:
				return
			}
		}
	}()
}

func (r *PullProgressRenderer) render(force bool) {
	if r.err != nil {
		return
	}
	lines := make([]string, 0, len(r.fileOrder))
	for _, id := range r.fileOrder {
		item := r.files[id]
		if item == nil {
			continue
		}
		lines = append(lines, r.renderLine(item))
	}
	r.err = r.base.Render(force, lines)
}

func (r *PullProgressRenderer) OnPlan(files []PullFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.planned = len(files) > 0
	r.files = make(map[string]*pullFileProgress, len(files))
	r.fileOrder = r.fileOrder[:0]
	for _, file := range files {
		r.files[file.Name] = &pullFileProgress{
			path:       file.Name,
			total:      file.Size,
			phase:      pullProgressPending,
			phaseSince: r.now(),
		}
		r.fileOrder = append(r.fileOrder, file.Name)
	}
	if r.planned {
		r.render(true)
	}
}

func (r *PullProgressRenderer) OnDownloadStart(file PullFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	item, ok := r.files[file.Name]
	if !ok {
		return
	}
	item.path = file.Name
	if file.Size > 0 {
		item.total = file.Size
	}
	item.phase = pullProgressDownloading
	item.phaseSince = r.now()
	item.lastBytes = time.Time{}
	r.render(false)
}

func (r *PullProgressRenderer) OnDownloadProgress(id string, bytesSoFar int64, total int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	item, ok := r.files[id]
	if !ok {
		return
	}
	if total > 0 {
		item.total = total
	}
	if bytesSoFar > item.current {
		item.current = bytesSoFar
		item.lastBytes = r.now()
	}
	item.phase = pullProgressDownloading
	if item.total > 0 && item.current >= item.total {
		item.phase = pullProgressVerifying
		item.phaseSince = r.now()
	}
	r.render(false)
}

func (r *PullProgressRenderer) OnCheckoutStart(file PullFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	item, ok := r.files[file.Name]
	if !ok {
		return
	}
	item.phase = pullProgressCheckingOut
	item.phaseSince = r.now()
	if item.total == 0 && file.Size > 0 {
		item.total = file.Size
	}
	r.render(false)
}

func (r *PullProgressRenderer) OnCompleted(file PullFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	item, ok := r.files[file.Name]
	if !ok {
		return
	}
	if item.total == 0 && file.Size > 0 {
		item.total = file.Size
	}
	if item.total > 0 {
		item.current = item.total
	}
	item.phase = pullProgressCompleted
	item.phaseSince = r.now()
	r.render(false)
}

func (r *PullProgressRenderer) Finish() error {
	r.mu.Lock()
	stop, done := r.stopHeartbeat, r.heartbeatDone
	r.stopHeartbeat, r.heartbeatDone = nil, nil
	r.mu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return r.err
	}
	lines := make([]string, 0, len(r.fileOrder))
	for _, id := range r.fileOrder {
		item := r.files[id]
		if item == nil {
			continue
		}
		lines = append(lines, r.renderLine(item))
	}
	if r.err == nil {
		r.err = r.base.Finish(lines)
	}
	r.planned = false
	return r.err
}

func (r *PullProgressRenderer) renderLine(file *pullFileProgress) string {
	label := "preparing pull"
	if file != nil && file.path != "" {
		label = TrimLabel(file.path, 48)
	}

	prefix := ""
	if file != nil {
		switch file.phase {
		case pullProgressDownloading, pullProgressCheckingOut:
			if !(file.total > 0 && file.current >= file.total) {
				prefix = r.base.Spinner() + " "
			}
		}
	}

	current := int64(0)
	total := int64(0)
	if file != nil {
		current = file.current
		total = file.total
	}
	bar := RenderProgressBar(current, total, 24)
	pct := RenderPercent(current, total)
	bytesLabel := RenderByteProgress(current, total, current >= total)
	phaseLabel := ""
	if file != nil {
		switch file.phase {
		case pullProgressVerifying:
			phaseLabel = " verifying (" + r.elapsed(file.phaseSince) + ")"
		case pullProgressCheckingOut:
			phaseLabel = " checking out (" + r.elapsed(file.phaseSince) + ")"
		case pullProgressCompleted:
			phaseLabel = " complete"
		case pullProgressPending:
			phaseLabel = " preparing (" + r.elapsed(file.phaseSince) + ")"
		case pullProgressDownloading:
			if file.lastBytes.IsZero() {
				phaseLabel = " waiting for data (" + r.elapsed(file.phaseSince) + ")"
			} else if r.now().Sub(file.lastBytes) >= 5*time.Second {
				phaseLabel = " no new data for " + r.elapsed(file.lastBytes)
			} else {
				phaseLabel = " downloading"
			}
		}
	}

	return fmt.Sprintf("%s%s %s %s %s%s", prefix, label, bar, pct, bytesLabel, phaseLabel)
}

func (r *PullProgressRenderer) elapsed(since time.Time) string {
	if since.IsZero() {
		return "0s"
	}
	elapsed := r.now().Sub(since)
	if elapsed < 0 {
		elapsed = 0
	}
	return elapsed.Truncate(time.Second).String()
}
