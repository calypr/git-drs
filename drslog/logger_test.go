package drslog

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestNewLoggerAndClose(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	logger, err := NewLogger("", false)
	if err != nil {
		t.Fatalf("NewLogger error: %v", err)
	}
	logger.Debug("hello world")
	logger.Debug("line")
	logger.Debug("another")

	logPath := filepath.Join(".git", "drs", "git-drs.log")
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected log file: %v", err)
	}

	Close()
}

func TestGetLoggerFallback(t *testing.T) {
	logger := GetLogger()
	if logger == nil {
		t.Fatalf("expected logger")
	}
}

func TestNewNoOpLogger(t *testing.T) {
	logger := NewNoOpLogger()
	logger.Debug("noop")
}

func TestNewLogger(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test.log")

	logger, err := NewLogger(logFile, false)
	if err != nil {
		t.Fatalf("NewLogger failed: %v", err)
	}
	if logger == nil {
		t.Fatal("Expected non-nil logger")
	}

	// Test writing to it
	logger.Info("test message")

	// Close logging to flush
	if err := Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}
	if !bytes.Contains(content, []byte("test message")) {
		t.Error("Log file missing message")
	}
}

func TestLogger_MessageOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("test message", "key", "value")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected logger output")
	}
	if !bytes.Contains([]byte(output), []byte("test message")) {
		t.Error("Expected output to contain 'test message'")
	}
}

func TestLogger_WithAttributes(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger = logger.With("component", "test", "version", "1.0")
	logger.Info("test message")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Expected output with attributes")
	}
}

func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")

	output := buf.String()

	// Debug and Info should be filtered out
	if bytes.Contains([]byte(output), []byte("debug")) {
		t.Error("Debug message should be filtered")
	}
	if bytes.Contains([]byte(output), []byte("info")) {
		t.Error("Info message should be filtered")
	}
	// Warn should be present
	if !bytes.Contains([]byte(output), []byte("warn")) {
		t.Error("Warn message should be present")
	}
}

func TestLogger_ErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	logger.Info("info")
	logger.Warn("warn")
	logger.Error("error")

	output := buf.String()

	// Only error should be present
	if bytes.Contains([]byte(output), []byte("info")) {
		t.Error("Info should be filtered")
	}
	if bytes.Contains([]byte(output), []byte("warn")) {
		t.Error("Warn should be filtered")
	}
	if !bytes.Contains([]byte(output), []byte("error")) {
		t.Error("Error should be present")
	}
}

func TestLogger_MultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	logger.Info("message 1")
	logger.Info("message 2")
	logger.Info("message 3")

	output := buf.String()

	if !bytes.Contains([]byte(output), []byte("message 1")) {
		t.Error("Expected message 1")
	}
	if !bytes.Contains([]byte(output), []byte("message 2")) {
		t.Error("Expected message 2")
	}
	if !bytes.Contains([]byte(output), []byte("message 3")) {
		t.Error("Expected message 3")
	}
}
