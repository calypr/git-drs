package pull

import (
	"fmt"
	"io"

	"github.com/calypr/git-drs/internal/progressui"
)

const pullNonTTYProgressInterval = progressui.NonTTYProgressInterval

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

type pullProgressRenderer struct {
	base      *progressui.Renderer
	planned   bool
	files     map[string]*pullFileProgress
	fileOrder []string
}

func newPullProgressRenderer(out io.Writer) *pullProgressRenderer {
	return &pullProgressRenderer{
		base:  progressui.NewRenderer(out),
		files: make(map[string]*pullFileProgress),
	}
}

func isPullWriterTTY(w io.Writer) bool {
	return progressui.IsWriterTTY(w)
}

func (r *pullProgressRenderer) render(force bool) {
	lines := make([]string, 0, len(r.fileOrder))
	for _, id := range r.fileOrder {
		item := r.files[id]
		if item == nil {
			continue
		}
		lines = append(lines, r.renderLine(item))
	}
	r.base.Render(force, lines)
}

func (r *pullProgressRenderer) OnPlan(files []pointerFile) {
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

func (r *pullProgressRenderer) OnDownloadStart(file pointerFile) {
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

func (r *pullProgressRenderer) OnDownloadProgress(id string, bytesSoFar int64, total int64) {
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

func (r *pullProgressRenderer) OnCheckoutStart(file pointerFile) {
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

func (r *pullProgressRenderer) OnCompleted(file pointerFile) {
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

func (r *pullProgressRenderer) Finish() {
	if !r.planned {
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
	r.base.Finish(lines)
	r.planned = false
}

func (r *pullProgressRenderer) renderLine(file *pullFileProgress) string {
	label := "preparing pull"
	if file != nil && file.path != "" {
		label = progressui.TrimLabel(file.path, 48)
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
	bar := progressui.RenderProgressBar(current, total, 24)
	pct := progressui.RenderPercent(current, total)
	bytesLabel := progressui.RenderByteProgress(current, total, current >= total)

	return fmt.Sprintf("%s%s %s %s %s", prefix, label, bar, pct, bytesLabel)
}
