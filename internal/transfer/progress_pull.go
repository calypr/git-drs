package transfer

import (
	"fmt"
	"io"
)

type pullProgressPhase string

const (
	pullProgressPending     pullProgressPhase = "pending"
	pullProgressDownloading pullProgressPhase = "downloading"
	pullProgressCheckingOut pullProgressPhase = "checking_out"
	pullProgressCompleted   pullProgressPhase = "completed"
)

type pullFileProgress struct {
	path    string
	total   int64
	current int64
	phase   pullProgressPhase
}

type PullProgressRenderer struct {
	base      *Renderer
	err       error
	planned   bool
	files     map[string]*pullFileProgress
	fileOrder []string
}

func NewPullProgressRenderer(out io.Writer) *PullProgressRenderer {
	return &PullProgressRenderer{
		base:  NewRenderer(out),
		files: make(map[string]*pullFileProgress),
	}
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
	r.planned = len(files) > 0
	r.files = make(map[string]*pullFileProgress, len(files))
	r.fileOrder = r.fileOrder[:0]
	for _, file := range files {
		r.files[file.Name] = &pullFileProgress{
			path:  file.Name,
			total: file.Size,
			phase: pullProgressPending,
		}
		r.fileOrder = append(r.fileOrder, file.Name)
	}
	if r.planned {
		r.render(true)
	}
}

func (r *PullProgressRenderer) OnDownloadStart(file PullFile) {
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
	r.render(false)
}

func (r *PullProgressRenderer) OnDownloadProgress(id string, bytesSoFar int64, total int64) {
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
	}
	item.phase = pullProgressDownloading
	r.render(false)
}

func (r *PullProgressRenderer) OnCheckoutStart(file PullFile) {
	if !r.planned {
		return
	}
	item, ok := r.files[file.Name]
	if !ok {
		return
	}
	item.phase = pullProgressCheckingOut
	if item.total == 0 && file.Size > 0 {
		item.total = file.Size
	}
	r.render(false)
}

func (r *PullProgressRenderer) OnCompleted(file PullFile) {
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
	r.render(false)
}

func (r *PullProgressRenderer) Finish() error {
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

	return fmt.Sprintf("%s%s %s %s %s", prefix, label, bar, pct, bytesLabel)
}
