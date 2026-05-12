package push

import (
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/calypr/git-drs/internal/pushsync"
	"github.com/mattn/go-isatty"
)

const nonTTYProgressInterval = 2 * time.Second

type uploadFileProgress struct {
	path      string
	started   bool
	completed bool
}

type uploadProgressRenderer struct {
	out           io.Writer
	isTTY         bool
	now           func() time.Time
	lastRender    time.Time
	mu            sync.Mutex
	planned       bool
	plan          pushsync.UploadPlanSummary
	files         map[string]*uploadFileProgress
	fileOrder     []string
	renderedLines int
	spinnerIndex  int
}

var spinnerFrames = []string{"|", "/", "-", `\`}

func newUploadProgressRenderer(out io.Writer) *uploadProgressRenderer {
	return &uploadProgressRenderer{
		out:   out,
		isTTY: isWriterTTY(out),
		now:   time.Now,
		files: make(map[string]*uploadFileProgress),
	}
}

func isWriterTTY(w io.Writer) bool {
	type fdWriter interface{ Fd() uintptr }
	f, ok := w.(fdWriter)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func (r *uploadProgressRenderer) OnUploadPlan(plan pushsync.UploadPlanSummary) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plan = plan
	r.planned = plan.TotalFiles > 0
	r.files = make(map[string]*uploadFileProgress, len(plan.Files))
	r.fileOrder = r.fileOrder[:0]
	r.renderedLines = 0
	r.spinnerIndex = 0
	for _, file := range plan.Files {
		r.files[file.OID] = &uploadFileProgress{
			path: file.Path,
		}
		r.fileOrder = append(r.fileOrder, file.OID)
	}
	if r.planned {
		r.renderLocked(true)
	}
}

func (r *uploadProgressRenderer) OnUploadProgress(ev pushsync.UploadProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	file, ok := r.files[ev.OID]
	if !ok {
		return
	}
	if ev.Path != "" {
		file.path = ev.Path
	}
	if ev.Phase == pushsync.UploadProgressUploading {
		file.started = true
	}
	if ev.Phase == pushsync.UploadProgressCompleted && !file.completed {
		file.started = true
		file.completed = true
	}
	r.renderLocked(false)
}

func (r *uploadProgressRenderer) Finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.planned {
		return
	}
	r.renderLocked(true)
	_, _ = fmt.Fprintln(r.out)
	r.planned = false
	r.renderedLines = 0
}

func (r *uploadProgressRenderer) renderLocked(force bool) {
	now := r.now()
	if !force && !r.isTTY && !r.lastRender.IsZero() && now.Sub(r.lastRender) < nonTTYProgressInterval {
		return
	}
	r.lastRender = now
	r.spinnerIndex = (r.spinnerIndex + 1) % len(spinnerFrames)

	lines := make([]string, 0, len(r.fileOrder))
	for idx, oid := range r.fileOrder {
		file := r.files[oid]
		if file == nil {
			continue
		}
		lines = append(lines, r.renderLine(idx, len(r.fileOrder), file))
	}

	if r.isTTY {
		if r.renderedLines > 0 {
			_, _ = fmt.Fprintf(r.out, "\x1b[%dA", r.renderedLines)
		}
		for _, line := range lines {
			_, _ = fmt.Fprintf(r.out, "\r\x1b[2K%s\n", line)
		}
		r.renderedLines = len(lines)
		return
	}

	for _, line := range lines {
		_, _ = fmt.Fprintln(r.out, line)
	}
}

func (r *uploadProgressRenderer) renderLine(idx int, total int, file *uploadFileProgress) string {
	label := "preparing upload"
	if file != nil && file.path != "" {
		label = trimProgressLabel(file.path, 48)
	}
	state := "pending"
	indicator := " "
	if file != nil {
		switch {
		case file.completed:
			state = "complete"
			indicator = "*"
		case file.started:
			state = "uploading"
			indicator = spinnerFrames[r.spinnerIndex]
		}
	}
	return fmt.Sprintf("[%s] %d/%d %s (%s)",
		indicator, idx+1, total, label, state)
}

func trimProgressLabel(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return "..." + s[len(s)-(max-3):]
}
