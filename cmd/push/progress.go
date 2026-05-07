package push

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/calypr/git-drs/internal/pushsync"
	"github.com/mattn/go-isatty"
)

const nonTTYProgressInterval = 2 * time.Second

type uploadFileProgress struct {
	path      string
	total     int64
	uploaded  int64
	completed bool
}

type uploadProgressRenderer struct {
	out          io.Writer
	isTTY        bool
	now          func() time.Time
	lastRender   time.Time
	mu           sync.Mutex
	planned      bool
	plan         pushsync.UploadPlanSummary
	files        map[string]*uploadFileProgress
	totalBytes   int64
	doneBytes    int64
	doneFiles    int
	currentLabel string
}

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
	r.totalBytes = 0
	r.doneBytes = 0
	r.doneFiles = 0
	r.currentLabel = ""
	r.files = make(map[string]*uploadFileProgress, len(plan.Files))
	for _, file := range plan.Files {
		r.files[file.OID] = &uploadFileProgress{
			path:  file.Path,
			total: file.Bytes,
		}
		r.totalBytes += file.Bytes
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
	if ev.TotalBytes > 0 {
		file.total = ev.TotalBytes
	}
	if file.total > 0 && ev.BytesSoFar > file.total {
		ev.BytesSoFar = file.total
	}
	if ev.BytesSoFar > file.uploaded {
		r.doneBytes += ev.BytesSoFar - file.uploaded
		file.uploaded = ev.BytesSoFar
	}
	if ev.Path != "" {
		r.currentLabel = ev.Path
	} else if file.path != "" {
		r.currentLabel = file.path
	}
	if ev.Phase == pushsync.UploadProgressCompleted && !file.completed {
		file.completed = true
		r.doneFiles++
		if file.total > 0 && file.uploaded < file.total {
			r.doneBytes += file.total - file.uploaded
			file.uploaded = file.total
		}
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
	if r.isTTY {
		_, _ = fmt.Fprintln(r.out)
	}
	r.planned = false
}

func (r *uploadProgressRenderer) renderLocked(force bool) {
	now := r.now()
	if !force && !r.isTTY && !r.lastRender.IsZero() && now.Sub(r.lastRender) < nonTTYProgressInterval {
		return
	}
	r.lastRender = now
	totalBytes := r.totalBytes
	doneBytes := r.doneBytes
	doneFiles := r.doneFiles
	totalFiles := r.plan.TotalFiles
	percent := 0.0
	if totalBytes > 0 {
		percent = (float64(doneBytes) / float64(totalBytes)) * 100
	}
	current := r.currentLabel
	if current == "" {
		current = "preparing uploads"
	}

	if r.isTTY {
		barWidth := 28
		filled := 0
		if totalBytes > 0 {
			filled = int((float64(doneBytes) / float64(totalBytes)) * float64(barWidth))
		}
		if filled > barWidth {
			filled = barWidth
		}
		bar := strings.Repeat("=", filled) + strings.Repeat(" ", barWidth-filled)
		line := fmt.Sprintf("\rUploading %d/%d files [%s] %5.1f%% %s/%s current: %s",
			doneFiles, totalFiles, bar, percent, humanBytes(doneBytes), humanBytes(totalBytes), trimProgressLabel(current, 48))
		_, _ = fmt.Fprint(r.out, line)
		return
	}

	line := fmt.Sprintf("Uploading %d/%d files (%.1f%%) %s/%s current=%s\n",
		doneFiles, totalFiles, percent, humanBytes(doneBytes), humanBytes(totalBytes), current)
	_, _ = fmt.Fprint(r.out, line)
}

func trimProgressLabel(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return "..." + s[len(s)-(max-3):]
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
