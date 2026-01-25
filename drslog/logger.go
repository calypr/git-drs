package drslog

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/calypr/git-drs/projectdir"
)

var globalLogger *slog.Logger
var globalLogFile io.Closer
var globalLoggerOnce sync.Once
var globalLoggerMu sync.RWMutex
var GIT_TRANSFER_TRACE int

func init() {
	GIT_TRANSFER_TRACE = 0
	if envValue := os.Getenv("GIT_TRANSFER_TRACE"); envValue != "" {
		if parsed, err := strconv.Atoi(envValue); err == nil {
			GIT_TRANSFER_TRACE = parsed
		}
	}
}

func TraceEnabled() bool {
	return GIT_TRANSFER_TRACE == 1
}

// NewLogger creates a new Logger that writes to the specified file and optionally stderr.
// It is safe to call this multiple times; only the first successful call sets the global logger.
func NewLogger(filename string, logToStderr bool) (*slog.Logger, error) {
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

	handler := slog.NewTextHandler(multiWriter, &slog.HandlerOptions{
		AddSource: true,
		Level:     resolveLogLevel(),
	})
	core := slog.New(handler).With("pid", os.Getpid())

	globalLoggerMu.Lock()
	globalLogFile = file
	globalLogger = core
	globalLoggerMu.Unlock()

	return globalLogger, nil
}

func GetLogger() *slog.Logger {
	globalLoggerOnce.Do(func() {
		if globalLogger == nil {
			globalLogger = NewNoOpLogger()
		}
	})
	return globalLogger
}

// Close closes the log file if open.
func Close() error {
	globalLoggerMu.Lock()
	defer globalLoggerMu.Unlock()
	if globalLogFile != nil {
		err := globalLogFile.Close()

		globalLogFile = nil
		return err
	}
	return nil
}

// NewNoOpLogger returns a logger that discards all output (useful for testing or fallback).
func NewNoOpLogger() *slog.Logger {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(handler)
}

func resolveLogLevel() slog.Level {
	if TraceEnabled() {
		return slog.LevelDebug
	}

	level, ok := readLogLevelFromGitConfig()
	if ok {
		return level
	}

	return slog.LevelDebug
}

func readLogLevelFromGitConfig() (slog.Level, bool) {
	cmd := exec.Command("git", "config", "--get", "lfs.customtransfer.drs.loglevel")
	output, err := cmd.Output()
	if err != nil {
		return slog.LevelDebug, false
	}

	value := strings.TrimSpace(string(output))
	if value == "" {
		return slog.LevelDebug, false
	}

	parsed, ok := parseLogLevel(value)
	if !ok {
		return slog.LevelDebug, false
	}
	return parsed, true
}

func parseLogLevel(value string) (slog.Level, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug, true
	case "INFO":
		return slog.LevelInfo, true
	case "WARN", "WARNING":
		return slog.LevelWarn, true
	case "ERROR":
		return slog.LevelError, true
	default:
		return slog.LevelDebug, false
	}
}
