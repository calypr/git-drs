package push

import (
	"fmt"
	"io"

	"github.com/calypr/git-drs/internal/progressui"
	"github.com/calypr/git-drs/internal/pushsync"
)

type uploadFileProgress struct {
	path      string
	total     int64
	current   int64
	started   bool
	completed bool
}

type uploadProgressRenderer struct {
	base      *progressui.Renderer
	planned   bool
	plan      pushsync.UploadPlanSummary
	files     map[string]*uploadFileProgress
	fileOrder []string
}

func newUploadProgressRenderer(out io.Writer) *uploadProgressRenderer {
	return &uploadProgressRenderer{
		base:  progressui.NewRenderer(out),
		files: make(map[string]*uploadFileProgress),
	}
}

func (r *uploadProgressRenderer) render(force bool) {
	lines := make([]string, 0, len(r.fileOrder))
	for idx, oid := range r.fileOrder {
		file := r.files[oid]
		if file == nil {
			continue
		}
		lines = append(lines, r.renderLine(idx, len(r.fileOrder), file))
	}
	r.base.Render(force, lines)
}

func (r *uploadProgressRenderer) OnUploadPlan(plan pushsync.UploadPlanSummary) {
	r.plan = plan
	r.planned = plan.TotalFiles > 0
	r.files = make(map[string]*uploadFileProgress, len(plan.Files))
	r.fileOrder = r.fileOrder[:0]
	for _, file := range plan.Files {
		r.files[file.OID] = &uploadFileProgress{
			path:  file.Path,
			total: file.Bytes,
		}
		r.fileOrder = append(r.fileOrder, file.OID)
	}
	if r.planned {
		r.render(true)
	}
}

func (r *uploadProgressRenderer) OnUploadProgress(ev pushsync.UploadProgressEvent) {
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
	if ev.BytesSoFar > file.current {
		file.current = ev.BytesSoFar
	}
	if ev.Phase == pushsync.UploadProgressUploading {
		file.started = true
	}
	if ev.Phase == pushsync.UploadProgressCompleted && !file.completed {
		file.started = true
		file.completed = true
		if file.total > 0 {
			file.current = file.total
		}
	}
	r.render(false)
}

func (r *uploadProgressRenderer) Finish() {
	if !r.planned {
		return
	}
	lines := make([]string, 0, len(r.fileOrder))
	for idx, oid := range r.fileOrder {
		file := r.files[oid]
		if file == nil {
			continue
		}
		lines = append(lines, r.renderLine(idx, len(r.fileOrder), file))
	}
	r.base.Finish(lines)
	r.planned = false
}

func (r *uploadProgressRenderer) HadUploads() bool {
	return r != nil && r.planned
}

func (r *uploadProgressRenderer) renderLine(idx int, total int, file *uploadFileProgress) string {
	label := "preparing upload"
	if file != nil && file.path != "" {
		label = progressui.TrimLabel(file.path, 48)
	}
	prefix := ""
	if file != nil {
		switch {
		case file.started && !file.completed:
			prefix = r.base.Spinner() + " "
		}
	}

	current := int64(0)
	totalBytes := int64(0)
	completed := false
	if file != nil {
		current = file.current
		totalBytes = file.total
		completed = file.completed
	}
	displayCurrent := progressui.VisibleProgressBytes(current, totalBytes, completed)
	bar := progressui.RenderProgressBar(displayCurrent, totalBytes, 24)
	pct := progressui.RenderPercentCapped(displayCurrent, totalBytes, completed)
	bytesLabel := progressui.RenderByteProgress(displayCurrent, totalBytes, completed)

	_ = idx
	_ = total
	return fmt.Sprintf("%s%s %s %s %s", prefix, label, bar, pct, bytesLabel)
}
