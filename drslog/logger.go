package drslog

import (
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/projectdir"
)

var log_file io.Closer
var global_logger *log.Logger

// NewLogger opens the log file and returns a Logger.
func NewLogger(filename string, logToStdout bool) (*log.Logger, error) {
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

	if logToStdout {
		writers = append(writers, os.Stdout)
	}

	multiWriter := io.MultiWriter(writers...)
	//TODO: make Lshortfile optional via config
	logger := log.New(multiWriter, "", log.LstdFlags|log.Lshortfile) // Standard log flags

	log_file = file
	global_logger = logger
	return logger, nil
}

func GetLogger() *log.Logger {
	return global_logger
}

func Close() {
	if log_file != nil {
		log_file.Close()
		log_file = nil
	}
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
