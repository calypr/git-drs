package drslog

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/calypr/git-drs/projectdir"
)

// Logger is a thread-safe wrapper around the standard *drslog.Logger.
type Logger struct {
	// Embed the standard logger
	*log.Logger

	// Mutex to protect concurrent calls (required when using Lshortfile/Llongfile)
	mu sync.Mutex
}

var (
	globalLogger *Logger
	mu           sync.Mutex // Protects globalLogger
	logFile      io.Closer
)

// NewLogger creates a new Logger that writes to the specified file and optionally stderr.
// It is safe to call this multiple times; only the first successful call sets the global logger.
func NewLogger(filename string, logToStderr bool) (*Logger, error) {
	mu.Lock()
	defer mu.Unlock()
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
	logFile = file
	writers = append(writers, file)

	if logToStderr {
		writers = append(writers, os.Stderr)
	}

	multiWriter := io.MultiWriter(writers...)

	// Create the core logger with Lshortfile for better debugging
	core := log.New(multiWriter, "", log.LstdFlags|log.Lshortfile)

	logger := &Logger{Logger: core}
	globalLogger = logger

	return logger, nil
}

// Thread-safe wrappers

func (l *Logger) Printf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Printf(format, v...)
}

func (l *Logger) Print(v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Print(v...)
}

func (l *Logger) Println(v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Println(v...)
}

func (l *Logger) Fatal(v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Fatal(v...)
}

func (l *Logger) Fatalf(format string, v ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Logger.Fatalf(format, v...)
}

func (l *Logger) Writer() io.Writer {
	return os.Stderr // or os.Stdout – sufficient for most library use cases
}

// GetLogger returns the global logger. Safe to call from multiple goroutines.
func GetLogger() *Logger {
	mu.Lock()
	if globalLogger == nil {
		mu.Unlock()
		// Fallback: create a no-op logger if not initialized. If errs then no logger for you
		logger, _ := NewLogger("", true)
		return logger
	}
	mu.Unlock()
	return globalLogger
}

// Close closes the log file if open.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// NewNoOpLogger returns a logger that discards all output (useful for testing or fallback).
func NewNoOpLogger() *Logger {
	return &Logger{
		Logger: log.New(io.Discard, "", 0),
	}
}
