package client

import (
	"fmt"
	"log"
	"os"
)

// Logger wraps a log.Logger and the file it writes to.
type Logger struct {
	file   *os.File
	logger *log.Logger
}

// NewLogger opens the log file and returns a Logger.
func NewLogger(filename string) (*Logger, error) {
	if filename == "" {
		filename = "transfer.log"
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	logger := log.New(file, "", log.LstdFlags) // Standard log flags
	return &Logger{file: file, logger: logger}, nil
}

// Log writes a formatted message to the log file.
func (l *Logger) Log(format string, args ...interface{}) {
	l.logger.Println(fmt.Sprintf(format, args...))
}

// Close closes the log file, flushing all writes.
func (l *Logger) Close() error {
	return l.file.Close()
}
