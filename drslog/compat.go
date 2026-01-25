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

func (w slogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		w.logger.Debug(message)
	}
	return len(p), nil
}
