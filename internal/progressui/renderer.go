package progressui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

const NonTTYProgressInterval = 2 * time.Second

var SpinnerFrames = []string{"|", "/", "-", `\`}

type Renderer struct {
	out           io.Writer
	isTTY         bool
	now           func() time.Time
	lastRender    time.Time
	mu            sync.Mutex
	active        bool
	renderedLines int
	spinnerIndex  int
}

func NewRenderer(out io.Writer) *Renderer {
	return &Renderer{
		out:   out,
		isTTY: IsWriterTTY(out),
		now:   time.Now,
	}
}

func IsWriterTTY(w io.Writer) bool {
	type fdWriter interface{ Fd() uintptr }
	f, ok := w.(fdWriter)
	if !ok {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}

func (r *Renderer) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

func (r *Renderer) SetTTY(isTTY bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.isTTY = isTTY
}

func (r *Renderer) Render(force bool, lines []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(lines) == 0 {
		return
	}

	now := r.now()
	if !force && !r.isTTY && !r.lastRender.IsZero() && now.Sub(r.lastRender) < NonTTYProgressInterval {
		return
	}
	r.lastRender = now
	r.spinnerIndex = (r.spinnerIndex + 1) % len(SpinnerFrames)
	r.active = true

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

func (r *Renderer) Finish(lines []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active {
		return
	}
	r.lastRender = r.now()
	if len(lines) > 0 {
		if r.isTTY {
			if r.renderedLines > 0 {
				_, _ = fmt.Fprintf(r.out, "\x1b[%dA", r.renderedLines)
			}
			for _, line := range lines {
				_, _ = fmt.Fprintf(r.out, "\r\x1b[2K%s\n", line)
			}
			r.renderedLines = len(lines)
		} else {
			for _, line := range lines {
				_, _ = fmt.Fprintln(r.out, line)
			}
		}
	}
	_, _ = fmt.Fprintln(r.out)
	r.active = false
	r.renderedLines = 0
}

func (r *Renderer) Spinner() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return SpinnerFrames[r.spinnerIndex]
}

func TrimLabel(s string, max int) string {
	if max <= 3 || len(s) <= max {
		return s
	}
	return "..." + s[len(s)-(max-3):]
}

func RenderProgressBar(current, total int64, width int) string {
	if width <= 0 {
		return "[]"
	}
	if total <= 0 {
		return "[" + Spaces(width) + "]"
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	filled := int((current * int64(width)) / total)
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("=", filled) + Spaces(width-filled) + "]"
}

func RenderPercent(current, total int64) string {
	if total <= 0 {
		return "  0.0%"
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	value := (float64(current) * 100.0) / float64(total)
	return fmt.Sprintf("%5.1f%%", value)
}

func RenderPercentCapped(current, total int64, completed bool) string {
	if total <= 0 {
		return "  0.0%"
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	value := (float64(current) * 100.0) / float64(total)
	if !completed && current < total && value > 99.9 {
		value = 99.9
	}
	return fmt.Sprintf("%5.1f%%", value)
}

func FormatBinaryBytes(n int64) string {
	if n <= 0 {
		return "0 B"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit && exp < 5; value /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix)
}

func VisibleProgressBytes(current, total int64, completed bool) int64 {
	if current < 0 {
		current = 0
	}
	if total <= 0 {
		return current
	}
	if current > total {
		current = total
	}
	if !completed && current >= total {
		return total - 1
	}
	return current
}

func RenderByteProgress(current, total int64, completed bool) string {
	formattedTotal := FormatBinaryBytes(total)
	formattedCurrent := FormatBinaryBytes(current)
	if !completed && total > 0 && current < total && formattedCurrent == formattedTotal {
		formattedCurrent = "<" + formattedTotal
	}
	return fmt.Sprintf("%s/%s", formattedCurrent, formattedTotal)
}

func Spaces(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(" ", count)
}
