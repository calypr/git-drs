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

// NewNoOpLogger creates a logger that discards all output.
// Returns a *log.Logger that writes to io.Discard.
func NewNoOpLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}
