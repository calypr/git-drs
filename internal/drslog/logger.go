package drslog

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"github.com/calypr/git-drs/internal/gitrepo"

	"github.com/calypr/syfon/client/logs"
)

var globalLogger *slog.Logger
var globalLogFile io.Closer
var globalLoggerOnce sync.Once
var globalLoggerMu sync.RWMutex
var GIT_TRANSFER_TRACE int
var modulePathSuffixOnce sync.Once
var modulePathSuffixValue string
var repoRootOnce sync.Once
var repoRootValue string

const (
	levelDebugStr   = "DEBUG"
	levelInfoStr    = "INFO"
	levelWarnStr    = "WARN"
	levelWarningStr = "WARNING"
	levelErrorStr   = "ERROR"
)

func init() {
	GIT_TRANSFER_TRACE = 0
	if envValue := os.Getenv("GIT_TRANSFER_TRACE"); envValue != "" {
		if parsed, err := strconv.Atoi(envValue); err == nil {
			GIT_TRANSFER_TRACE = parsed
		}
	}
}

// TraceEnabled reports whether transfer trace logging is enabled.
func TraceEnabled() bool {
	return GIT_TRANSFER_TRACE == 1
}

// NewLogger creates the global slog logger, writing to filename and optionally stderr.
// An empty filename uses the repository's default log path.
func NewLogger(filename string, logToStderr bool) (*slog.Logger, error) {
	var writers []io.Writer

	if filename == "" {
		// create drs dir if it doesn't exist
		if err := os.MkdirAll(gitrepo.DRSDir, 0755); err != nil {
			return nil, err
		}

		filename = gitrepo.DRSLogFile
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
		AddSource:   true,
		Level:       resolveLogLevel(),
		ReplaceAttr: replaceSourceAttr,
	})
	core := slog.New(logs.NewProgressHandler(handler)).With("pid", os.Getpid())

	globalLoggerMu.Lock()
	globalLogFile = file
	globalLogger = core
	globalLoggerMu.Unlock()

	return globalLogger, nil
}

// GetLogger returns the global logger, initializing a no-op logger when needed.
func GetLogger() *slog.Logger {
	globalLoggerOnce.Do(func() {
		if globalLogger == nil {
			globalLogger = NewNoOpLogger()
		}
	})
	return globalLogger
}

// Close closes the active log file, if one was opened.
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

// NewNoOpLogger returns a logger that discards all output.
func NewNoOpLogger() *slog.Logger {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(logs.NewProgressHandler(handler))
}

// resolveLogLevel returns the configured log level, or info by default.
func resolveLogLevel() slog.Level {
	if TraceEnabled() {
		return slog.LevelDebug
	}

	level, ok := readLogLevelFromGitConfig()
	if ok {
		return level
	}

	return slog.LevelInfo
}

// readLogLevelFromGitConfig reads drs.loglevel from the repository config.
// It returns false when the setting is absent or invalid.
func readLogLevelFromGitConfig() (slog.Level, bool) {
	val, err := gitrepo.GetGitConfigString("drs.loglevel")
	if err != nil || val == "" {
		return slog.LevelInfo, false
	}

	parsed, ok := parseLogLevel(val)
	if !ok {
		return slog.LevelInfo, false
	}
	return parsed, true
}

// parseLogLevel maps a textual level name to slog.Level.
func parseLogLevel(value string) (slog.Level, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case levelDebugStr:
		return slog.LevelDebug, true
	case levelInfoStr:
		return slog.LevelInfo, true
	case levelWarnStr, levelWarningStr:
		return slog.LevelWarn, true
	case levelErrorStr:
		return slog.LevelError, true
	default:
		return slog.LevelDebug, false
	}
}

// replaceSourceAttr shortens source paths in log records.
func replaceSourceAttr(_ []string, attr slog.Attr) slog.Attr {
	if attr.Key != slog.SourceKey {
		return attr
	}
	source, ok := attr.Value.Any().(*slog.Source)
	if !ok || source == nil {
		return attr
	}
	source.File = formatSourcePath(source.File)
	attr.Value = slog.AnyValue(source)
	return attr
}

// formatSourcePath shortens file paths using the module suffix or repository root.
func formatSourcePath(path string) string {
	pathSlash := filepath.ToSlash(path)
	moduleSuffix := modulePathSuffix()
	if moduleSuffix != "" {
		moduleSuffixSlash := strings.TrimPrefix(filepath.ToSlash(moduleSuffix), "/")
		if idx := strings.Index(pathSlash, "/"+moduleSuffixSlash+"/"); idx >= 0 {
			return pathSlash[idx+1:]
		}
		if strings.HasPrefix(pathSlash, moduleSuffixSlash+"/") {
			return pathSlash
		}
	}
	repoRoot := repoRootPath()
	if repoRoot != "" {
		repoRootSlash := filepath.ToSlash(repoRoot)
		if strings.HasPrefix(pathSlash, repoRootSlash+"/") {
			rel := strings.TrimPrefix(pathSlash, repoRootSlash+"/")
			if moduleSuffix != "" {
				return filepath.ToSlash(filepath.Join(moduleSuffix, rel))
			}
			return rel
		}
	}
	return pathSlash
}

// modulePathSuffix returns the module suffix derived from build info.
func modulePathSuffix() string {
	modulePathSuffixOnce.Do(func() {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Path != "" {
			parts := strings.Split(info.Main.Path, "/")
			if len(parts) > 1 {
				modulePathSuffixValue = strings.Join(parts[1:], "/")
			}
		}
	})
	return modulePathSuffixValue
}

// repoRootPath finds and caches the repository root by searching for go.mod.
func repoRootPath() string {
	repoRootOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			return
		}
		dir := cwd
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				repoRootValue = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return
			}
			dir = parent
		}
	})
	return repoRootValue
}
