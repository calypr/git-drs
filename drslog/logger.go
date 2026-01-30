// go
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

	"github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/gitrepo"

	"github.com/calypr/data-client/logs"
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

// init initializes package-level settings from the environment.
//
// Documented calls inside:
//   - os.Getenv("GIT_TRANSFER_TRACE")
//     Reads environment variable to optionally enable trace logging.
//   - strconv.Atoi(envValue)
//     Parses the numeric env value.
//
// Side-effects:
// - sets package variable GIT_TRANSFER_TRACE.
// Typical callers:
// - runtime automatically invokes init(); no external callers needed.
func init() {
	GIT_TRANSFER_TRACE = 0
	if envValue := os.Getenv("GIT_TRANSFER_TRACE"); envValue != "" {
		if parsed, err := strconv.Atoi(envValue); err == nil {
			GIT_TRANSFER_TRACE = parsed
		}
	}
}

// TraceEnabled returns whether transfer trace logging is enabled.
//
// Documented calls inside:
// - reads package variable GIT_TRANSFER_TRACE.
// Typical callers:
// - logging setup and callsites that want to be verbose only when trace is enabled.
func TraceEnabled() bool {
	return GIT_TRANSFER_TRACE == 1
}

// NewLogger creates and installs a global slog.Logger that writes to the specified file
// and optionally to stderr. It is safe to call multiple times; the first successful call
// establishes the global logger.
//
// Documented calls inside:
//   - projectdir.DRS_DIR usage
//     Uses projectdir.DRS_DIR to create log directory.
//   - os.MkdirAll(projectdir.DRS_DIR, 0755)
//     Ensures the directory exists; returns error on failure.
//   - filepath.Join(projectdir.DRS_DIR, "git-drs.log")
//     Constructs default filename when none provided.
//   - os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
//     Opens/creates the log file (returns *os.File).
//   - io.MultiWriter(writers...)
//     Combines file and optionally os.Stderr into a single Writer.
//   - slog.NewTextHandler(multiWriter, &slog.HandlerOptions{...})
//     Creates the text handler for slog that writes to the combined writer.
//   - slog.New(handler).With("pid", os.Getpid())
//     Builds the logger and attaches pid attribute.
//   - globalLoggerMu.Lock()/Unlock()
//     Protects globalLogFile and globalLogger assignment.
//
// Side-effects:
// - sets package-level globalLogger and globalLogFile.
// Typical callers:
// - application startup code that wants to initialize logging (e.g. main).
func NewLogger(filename string, logToStderr bool) (*slog.Logger, error) {
	var writers []io.Writer

	if filename == "" {
		// create drs dir if it doesn't exist
		if err := os.MkdirAll(common.DRS_DIR, 0755); err != nil {
			return nil, err
		}

		filename = filepath.Join(common.DRS_DIR, "git-drs.log")
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

// GetLogger returns the global logger, initializing a no-op logger on first access.
//
// Documented calls inside:
//   - globalLoggerOnce.Do(func() { ... })
//     Ensures initialization runs only once.
//   - NewNoOpLogger()
//     Creates a logger that discards output if no global logger was set.
//
// Typical callers:
// - any package code that needs access to the package-level logger.
func GetLogger() *slog.Logger {
	globalLoggerOnce.Do(func() {
		if globalLogger == nil {
			globalLogger = NewNoOpLogger()
		}
	})
	return globalLogger
}

// Close closes the active log file if one was opened.
//
// Documented calls inside:
//   - globalLoggerMu.Lock()/Unlock()
//     Protects access to globalLogFile.
//   - globalLogFile.Close()
//     Closes the underlying file and returns any error.
//
// Side-effects:
// - sets globalLogFile to nil.
// Typical callers:
// - application shutdown code or tests that want to release file handles.
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
//
// Documented calls inside:
//   - slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})
//     Creates a text handler writing to io.Discard.
//   - slog.New(handler)
//     Builds the logger.
//
// Typical callers:
// - GetLogger on first access when no global logger was configured.
// - tests that need a deterministic logger that produces no output.
func NewNoOpLogger() *slog.Logger {
	handler := slog.NewTextHandler(io.Discard, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	return slog.New(logs.NewProgressHandler(handler))
}

// resolveLogLevel determines the effective log level.
//
// Documented calls inside:
//   - TraceEnabled()
//     If trace is enabled, returns Debug level immediately.
//   - readLogLevelFromGitConfig()
//     Attempts to read configured level from git config; returns level and ok.
//   - defaults to slog.LevelInfo when nothing else matches.
//
// Typical callers:
// - NewLogger -> used when creating slog.HandlerOptions.Level.
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

// readLogLevelFromGitConfig queries git configuration for a custom log level.
//
// Documented calls inside:
//   - exec.Command("git", "config", "--get", "lfs.customtransfer.drs.loglevel")
//     Constructs the command to query git config.
//   - cmd.Output()
//     Executes the command and returns raw output or an error.
//   - strings.TrimSpace(string(output))
//     Trims whitespace/newlines from git output.
//   - parseLogLevel(value)
//     Parses the trimmed value into a slog.Level.
//
// Behavior:
// - On any error or empty output, returns (slog.LevelInfo, false) to indicate no valid config was found.
// Typical callers:
// - resolveLogLevel when initializing a logger.
func readLogLevelFromGitConfig() (slog.Level, bool) {
	val, err := gitrepo.GetGitConfigString("lfs.customtransfer.drs.loglevel")
	if err != nil || val == "" {
		return slog.LevelInfo, false
	}

	parsed, ok := parseLogLevel(val)
	if !ok {
		return slog.LevelInfo, false
	}
	return parsed, true
}

// parseLogLevel maps textual level names to slog.Level.
//
// Documented calls inside:
//   - strings.ToUpper(strings.TrimSpace(value))
//     Normalizes the input for comparison.
//   - switch on normalized value to return corresponding slog.Level constants.
//
// Typical callers:
// - readLogLevelFromGitConfig
// - resolveLogLevel indirectly.
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

// replaceSourceAttr rewrites the slog.Source attr to a shorter path suitable for logs.
//
// Documented calls inside:
//   - attr.Key comparison with slog.SourceKey
//     Determines whether the attribute is the source attribute.
//   - attr.Value.Any().(*slog.Source)
//     Retrieves and type-asserts the attribute value to *slog.Source.
//   - formatSourcePath(source.File)
//     Formats the file path according to module or repo root heuristics.
//   - attr.Value = slog.AnyValue(source)
//     Replaces attribute value with modified source.
//
// Typical callers:
// - passed as ReplaceAttr to slog.HandlerOptions in NewLogger.
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

// formatSourcePath shortens file paths using module suffix or repo root heuristics.
//
// Documented calls inside:
//   - filepath.ToSlash(path)
//     Normalizes OS-specific separators to forward slashes.
//   - modulePathSuffix()
//     Gets the module path suffix (derived from build info).
//   - strings.TrimPrefix(filepath.ToSlash(moduleSuffix), "/")
//     Normalizes module suffix.
//   - strings.Index(pathSlash, "/"+moduleSuffixSlash+"/")
//     Searches for module suffix within the path to trim leading segments.
//   - strings.HasPrefix(pathSlash, moduleSuffixSlash+"/")
//     Handles case where path already starts with module suffix.
//   - repoRootPath()
//     Attempts to resolve the repository root (by locating go.mod).
//   - strings.HasPrefix(pathSlash, repoRootSlash+"/")
//     Trims repository-root prefix to produce a relative path.
//   - filepath.ToSlash(filepath.Join(moduleSuffix, rel))
//     Reconstructs path when combining module suffix and relative path.
//
// Typical callers:
// - replaceSourceAttr when rewriting source file paths for log output.
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

// modulePathSuffix returns the module path suffix derived from build info.
//
// Documented calls inside:
//   - runtime/debug.ReadBuildInfo()
//     Reads build info at runtime; used to extract Main.Path.
//   - strings.Split(info.Main.Path, "/")
//     Splits module path to drop the first element (typically hostname).
//
// Side-effects:
// - caches computed value via modulePathSuffixOnce.
// Typical callers:
// - formatSourcePath to help shorten file paths.
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

// repoRootPath attempts to locate the repository root by searching for go.mod upward.
//
// Documented calls inside:
//   - os.Getwd()
//     Retrieves current working directory as a starting point.
//   - os.Stat(filepath.Join(dir, "go.mod"))
//     Checks for presence of go.mod in each directory while walking up.
//   - filepath.Dir(dir)
//     Moves up one directory level on each iteration.
//
// Side-effects:
// - caches resolved repo root via repoRootOnce.
// Typical callers:
// - formatSourcePath when computing shorter file paths for logs.
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
