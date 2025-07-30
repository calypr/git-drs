package client

import (
	"log"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/config"
)

// Logger wraps a log.Logger and the file it writes to.
type Logger struct {
	file   *os.File
	logger *log.Logger
}

// NewLogger opens the log file and returns a Logger.
func NewLogger(filename string) (*Logger, error) {
	if filename == "" {
		filename = filepath.Join(config.DRS_DIR, "transfer.log")
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	logger := log.New(file, "", log.LstdFlags) // Standard log flags
	return &Logger{file: file, logger: logger}, nil
}

// Log writes a formatted message to the log file.
func (l *Logger) Log(args ...any) {
	l.logger.Println(args...)
}

func (l *Logger) Logf(format string, args ...any) {
	l.logger.Printf(format, args...)
}

// Close closes the log file, flushing all writes.
func (l *Logger) Close() error {
	return l.file.Close()
}

type NoOpLogger struct{}

// Logf implements the Logf method for NoOpLogger, doing nothing.
func (n *NoOpLogger) Logf(format string, v ...any) {
}
func (n *NoOpLogger) Log(args ...any) {
}

// Close implements the Close method for NoOpLogger, doing nothing.
func (n *NoOpLogger) Close() error {
	return nil
}

type LoggerInterface interface {
	Logf(format string, v ...any)
	Log(args ...any)
	Close() error // If close is part of the interface
}
