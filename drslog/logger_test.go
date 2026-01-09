package drslog

import (
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
	logger.Printf("hello %s", "world")
	logger.Print("line")
	logger.Println("another")

	logPath := filepath.Join(".drs", "git-drs.log")
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
	logger.Print("noop")
}
