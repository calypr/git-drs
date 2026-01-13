package drslog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/calypr/git-drs/projectdir"
)

var globalLogger *log.Logger
var globalLogFile io.Closer
var globalLoggerOnce sync.Once
var globalLoggerMu sync.RWMutex

// NewLogger creates a new Logger that writes to the specified file and optionally stderr.
// It is safe to call this multiple times; only the first successful call sets the global logger.
func NewLogger(filename string, logToStderr bool) (*log.Logger, error) {
	var writers []io.Writer

	if filename == "" {
		//create drs dir if it doesn't exist
		if err := os.MkdirAll(projectdir.DRS_DIR, 0755); err != nil {
			return nil, err
		}

		filename = filepath.Join(projectdir.DRS_DIR, "git-drs.log") // Assuming transfer.log is a variable
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	writers = append(writers, file)

	if logToStderr {
		writers = append(writers, os.Stderr)
	}

	multiWriter := io.MultiWriter(writers...)

	// Create the core logger with Lshortfile for better debugging
	// Prefix log entries with PID for easier tracing in multi-process scenarios
	prefix := fmt.Sprintf("[%d] ", os.Getpid())
	core := log.New(multiWriter, prefix, log.LstdFlags|log.Lshortfile)

	globalLoggerMu.Lock()
	globalLogFile = file
	globalLogger = core
	globalLoggerMu.Unlock()

	return globalLogger, nil
}

func GetLogger() *log.Logger {
	globalLoggerOnce.Do(func() {
		globalLoggerMu.Lock()
		if globalLogger == nil {
			globalLogger = NewNoOpLogger()
		}
		globalLoggerMu.Unlock()
	})
	globalLoggerMu.RLock()
	defer globalLoggerMu.RUnlock()
	return globalLogger
}

// Close closes the log file if open.
func Close() {
	globalLoggerMu.Lock()
	defer globalLoggerMu.Unlock()
	if globalLogFile != nil {
		globalLogFile.Close()
		globalLogFile = nil
	}
}

// NewNoOpLogger returns a logger that discards all output (useful for testing or fallback).
func NewNoOpLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
