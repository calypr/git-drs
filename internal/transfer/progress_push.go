package transfer

import (
	"fmt"
	"io"
	"sync"
)

type uploadFileProgress struct {
	path      string
	total     int64
	current   int64
	started   bool
	completed bool
}

type metadataProgress struct {
	total     int
	completed int
	active    bool
	done      bool
}

type UploadProgressRenderer struct {
	mu        sync.Mutex
	base      *Renderer
	err       error
	planned   bool
	plan      UploadPlanSummary
	metadata  metadataProgress
	files     map[string]*uploadFileProgress
	fileOrder []string
}

func NewUploadProgressRenderer(out io.Writer) *UploadProgressRenderer {
	return &UploadProgressRenderer{
		base:  NewRenderer(out),
		files: make(map[string]*uploadFileProgress),
	}
}

func (r *UploadProgressRenderer) renderLocked(force bool) {
	if r.err != nil {
		return
	}
	lines := make([]string, 0, len(r.fileOrder)+1)
	if r.metadata.total > 0 {
		lines = append(lines, r.renderMetadataLine())
	}
	for idx, oid := range r.fileOrder {
		file := r.files[oid]
		if file == nil {
			continue
		}
		lines = append(lines, r.renderLine(idx, len(r.fileOrder), file))
	}
	r.err = r.base.Render(force, lines)
}

func (r *UploadProgressRenderer) OnMetadataPlan(plan MetadataPlanSummary) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.metadata = metadataProgress{
		total:  plan.TotalObjects,
		active: plan.TotalObjects > 0,
		done:   false,
	}
	if plan.TotalObjects > 0 {
		r.renderLocked(true)
	}
}

func (r *UploadProgressRenderer) OnMetadataProgress(ev MetadataProgressEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ev.Total > 0 {
		r.metadata.total = ev.Total
	}
	if ev.Completed > r.metadata.completed {
		r.metadata.completed = ev.Completed
	}
	if ev.Phase == MetadataProgressRegistering {
		r.metadata.active = true
	}
	if ev.Phase == MetadataProgressCompleted {
		r.metadata.active = false
		r.metadata.done = true
		if r.metadata.total > 0 {
			r.metadata.completed = r.metadata.total
		}
	}
	if r.metadata.total > 0 {
		r.renderLocked(false)
	}
}

func (r *UploadProgressRenderer) OnUploadPlan(plan UploadPlanSummary) {
	r.mu.Lock()
	defer r.mu.Unlock()

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
		r.renderLocked(true)
	}
}

func (r *UploadProgressRenderer) OnUploadProgress(ev UploadProgressEvent) {
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
	if ev.BytesSoFar > file.current {
		file.current = ev.BytesSoFar
	}
	if ev.Phase == UploadProgressUploading {
		file.started = true
	}
	if ev.Phase == UploadProgressCompleted && !file.completed {
		file.started = true
		file.completed = true
		if file.total > 0 {
			file.current = file.total
		}
	}
	r.renderLocked(false)
}

func (r *UploadProgressRenderer) Finish() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.planned && r.metadata.total == 0 {
		return r.err
	}
	lines := make([]string, 0, len(r.fileOrder))
	if r.metadata.total > 0 {
		lines = append(lines, r.renderMetadataLine())
	}
	for idx, oid := range r.fileOrder {
		file := r.files[oid]
		if file == nil {
			continue
		}
		lines = append(lines, r.renderLine(idx, len(r.fileOrder), file))
	}
	if r.err == nil {
		r.err = r.base.Finish(lines)
	}
	r.planned = false
	r.metadata = metadataProgress{}
	return r.err
}

func (r *UploadProgressRenderer) HadUploads() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r != nil && r.planned
}

func (r *UploadProgressRenderer) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

func (r *UploadProgressRenderer) renderLine(_ int, _ int, file *uploadFileProgress) string {
	label := "preparing upload"
	if file != nil && file.path != "" {
		label = TrimLabel(file.path, 48)
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
	displayCurrent := VisibleProgressBytes(current, totalBytes, completed)
	bar := RenderProgressBar(displayCurrent, totalBytes, 24)
	pct := RenderPercentCapped(displayCurrent, totalBytes, completed)
	bytesLabel := RenderByteProgress(displayCurrent, totalBytes, completed)

	return fmt.Sprintf("%s%s %s %s %s", prefix, label, bar, pct, bytesLabel)
}

func (r *UploadProgressRenderer) renderMetadataLine() string {
	total := r.metadata.total
	completed := r.metadata.completed
	if completed < 0 {
		completed = 0
	}
	if total > 0 && completed > total {
		completed = total
	}
	prefix := ""
	if r.metadata.active && !r.metadata.done {
		prefix = r.base.Spinner() + " "
	}
	bar := RenderProgressBar(int64(completed), int64(total), 24)
	pct := RenderPercent(int64(completed), int64(total))
	return fmt.Sprintf("%sregistering metadata %s %s %d/%d", prefix, bar, pct, completed, total)
}
