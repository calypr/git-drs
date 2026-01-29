package drslog

import (
	"log"
	"log/slog"
	"strings"
)

// AsStdLogger adapts a slog.Logger to the standard library log.Logger interface.
func AsStdLogger(logger *slog.Logger) *log.Logger {
	return log.New(slogWriter{logger: logger}, "", 0)
}

type slogWriter struct {
	logger *slog.Logger
}

// Write implements io.Writer and is invoked by the standard library log functions
// (for example, the logger returned by AsStdLogger). Documented calls inside:
//
//   - `string(p)`
//     Converts the incoming byte slice to a string for processing.
//
//   - `strings.TrimSpace(...)`
//     Removes leading/trailing whitespace (including trailing newlines) so that
//     empty or whitespace-only log lines are dropped.
//
//   - `w.logger.Info(...)`
//     Emits the trimmed message to the wrapped `slog.Logger` at Info level.
//
//   - `len(p)`
//     Returned to satisfy the io.Writer contract; indicates the number of bytes
//     "written". This implementation never returns an error.
//
// Note: This implementation deliberately ignores write errors and drops messages
// that are empty after trimming.
func (w slogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		w.logger.Info(message)
	}
	return len(p), nil
}
