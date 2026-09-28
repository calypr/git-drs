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
	pullProgressResolving   pullProgressPhase = "resolving"
	pullProgressConnecting  pullProgressPhase = "connecting"
	pullProgressWaiting     pullProgressPhase = "waiting"
	pullProgressExternal    pullProgressPhase = "external"
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
	stage             string
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
	r.stage = "Checking local object cache"
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

func (r *PullProgressRenderer) OnStage(stage string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stage = stage
	for _, item := range r.files {
		if item.phase == pullProgressPending {
			item.phaseSince = r.now()
		}
	}
	r.render(false)
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
	item.phase = pullProgressResolving
	item.phaseSince = r.now()
	item.lastBytes = time.Time{}
	r.render(false)
}

func (r *PullProgressRenderer) OnTransferStart(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.files[id]
	if item == nil || !r.planned || item.phase == pullProgressDownloading {
		return
	}
	item.phase = pullProgressWaiting
	item.phaseSince = r.now()
	r.render(false)
}

func (r *PullProgressRenderer) OnConnectionStart(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.files[id]
	if item == nil || !r.planned || item.current > 0 {
		return
	}
	item.phase = pullProgressConnecting
	item.phaseSince = r.now()
	r.render(false)
}

func (r *PullProgressRenderer) OnDownloadRestart(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.files[id]
	if item == nil || !r.planned {
		return
	}
	item.current = 0
	item.lastBytes = time.Time{}
	item.phase = pullProgressConnecting
	item.phaseSince = r.now()
	r.render(false)
}

func (r *PullProgressRenderer) OnExternalTransferStart(file PullFile) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.files[file.Name]
	if item == nil || !r.planned {
		return
	}
	item.phase = pullProgressExternal
	item.phaseSince = r.now()
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
	if file == nil {
		return ""
	}
	label := TrimLabel(file.path, 48)
	if file.phase == pullProgressCompleted {
		return label + ": complete"
	}
	prefix := r.base.Spinner() + " "
	switch file.phase {
	case pullProgressPending:
		return fmt.Sprintf("%s%s: %s (%s)", prefix, label, r.stage, r.elapsed(file.phaseSince))
	case pullProgressResolving:
		return fmt.Sprintf("%s%s: Resolving download source (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressConnecting:
		return fmt.Sprintf("%s%s: Opening download connection (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressWaiting:
		return fmt.Sprintf("%s%s: Connected; waiting for data (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressExternal:
		return fmt.Sprintf("%s%s: Globus transfer running (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressVerifying:
		return fmt.Sprintf("%s%s: Verifying download (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressCheckingOut:
		return fmt.Sprintf("%s%s: Checking out file (%s)", prefix, label, r.elapsed(file.phaseSince))
	case pullProgressDownloading:
		status := "downloading"
		if !file.lastBytes.IsZero() && r.now().Sub(file.lastBytes) >= 5*time.Second {
			status = "no new data for " + r.elapsed(file.lastBytes)
		}
		return fmt.Sprintf("%s%s %s %s %s %s", prefix, label, RenderProgressBar(file.current, file.total, 24), RenderPercent(file.current, file.total), RenderByteProgress(file.current, file.total, false), status)
	}
	return label
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
