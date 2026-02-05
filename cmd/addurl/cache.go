package addurl

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/precommit_cache"
)

// updatePrecommitCache updates the project's pre-commit cache with a mapping
// from a repository-relative `pathArg` to the given LFS `oid` and records the
// external source URL. It will:
//   - require a non-nil `logger`
//   - open the pre-commit cache (`precommit_cache.Open`)
//   - ensure cache directories exist
//   - convert the supplied worktree path to a repository-relative path
//   - create or update the per-path JSON entry with the current OID and timestamp
//   - create or update the per-OID JSON entry listing paths that reference it,
//     the external URL, and a content-change flag when the path's OID changed
//   - remove the path from the previous OID entry when the content changed
//
// Parameters:
//   - ctx: context for operations that may be cancellable
//   - logger: a non-nil `*slog.Logger` used for warnings; if nil the function
//     returns an error
//   - pathArg: the worktree path to record (absolute or relative); must not be empty
//   - oid: the LFS object id (string) to associate with the path
//   - externalURL: optional external source URL for the object; empty string is allowed
//
// Returns an error if any cache operation, path resolution, or I/O fails.
func updatePrecommitCache(ctx context.Context, logger *slog.Logger, pathArg, oid, externalURL string) error {
	if logger == nil {
		return errors.New("logger is required")
	}
	// Open pre-commit cache. Returns a configured Cache or error.
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}

	// Ensure cache directories exist.
	if err := ensureCacheDirs(cache, logger); err != nil {
		return err
	}

	// Convert worktree path to repository-relative path.
	relPath, err := repoRelativePath(pathArg)
	if err != nil {
		return err
	}

	// Current timestamp in RFC3339 format (UTC).
	now := time.Now().UTC().Format(time.RFC3339)

	// Read previous path entry, if any.
	pathFile := cachePathEntryFile(cache, relPath)
	prevEntry, prevExists, err := readPathEntry(pathFile)
	if err != nil {
		return err
	}
	// track whether content changed for this path
	contentChanged := prevExists && prevEntry.LFSOID != "" && prevEntry.LFSOID != oid

	if err := writeJSONAtomic(pathFile, precommit_cache.PathEntry{
		Path:      relPath,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	if err := upsertOIDEntry(cache, oid, relPath, externalURL, now, contentChanged); err != nil {
		return err
	}

	if contentChanged {
		_ = removePathFromOID(cache, prevEntry.LFSOID, relPath, now)
	}

	return nil
}

// ensureCacheDirs verifies and creates the pre-commit cache directory layout
// (paths and oids directories). It logs a warning when creating a missing
// cache root.
func ensureCacheDirs(cache *precommit_cache.Cache, logger *slog.Logger) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	if _, err := os.Stat(cache.Root); err != nil {
		if os.IsNotExist(err) {
			logger.Warn("pre-commit cache directory missing; creating", "path", cache.Root)
		} else {
			return err
		}
	}
	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		return fmt.Errorf("create cache paths dir: %w", err)
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		return fmt.Errorf("create cache oids dir: %w", err)
	}
	return nil
}

// repoRelativePath converts a worktree path (absolute or relative) to a
// repository-relative path. It resolves symlinks and ensures the path is
// contained within the repository root.
func repoRelativePath(pathArg string) (string, error) {
	if pathArg == "" {
		return "", errors.New("empty worktree path")
	}
	root, err := gitrepo.GitTopLevel()
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(pathArg)
	if filepath.IsAbs(clean) {
		clean, err = filepath.EvalSymlinks(clean)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path %s is outside repo root %s", clean, root)
		}
		return filepath.ToSlash(rel), nil
	}
	return filepath.ToSlash(clean), nil
}

// cachePathEntryFile returns the filesystem path to the JSON path-entry file
// for the given repository-relative path within the provided Cache.
func cachePathEntryFile(cache *precommit_cache.Cache, path string) string {
	return filepath.Join(cache.PathsDir, precommit_cache.EncodePath(path)+".json")
}

// cacheOIDEntryFile returns the filesystem path to the JSON OID-entry file
// for the given LFS OID. The file is named by sha256(oid) to avoid filesystem
// restrictions and collisions.
func cacheOIDEntryFile(cache *precommit_cache.Cache, oid string) string {
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(cache.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
}

// readPathEntry reads and parses a JSON PathEntry from disk. It returns the
// parsed entry, a boolean indicating existence, or an error on I/O/parse
// failure.
func readPathEntry(path string) (*precommit_cache.PathEntry, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var entry precommit_cache.PathEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false, err
	}
	return &entry, true, nil
}

// readOIDEntry reads and parses a JSON OIDEntry from disk. If the file is
// missing it returns a freshly initialized entry (with LFSOID set to the
// supplied oid and UpdatedAt set to now).
func readOIDEntry(path string, oid string, now string) (*precommit_cache.OIDEntry, error) {
	entry := &precommit_cache.OIDEntry{
		LFSOID:    oid,
		Paths:     []string{},
		UpdatedAt: now,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return entry, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, entry); err != nil {
		return nil, err
	}
	entry.LFSOID = oid
	return entry, nil
}

// upsertOIDEntry creates or updates the OID entry for `oid`, ensuring `path`
// is listed among its Paths, updating ExternalURL when provided, and setting
// content-change/state fields. The updated entry is written atomically.
func upsertOIDEntry(cache *precommit_cache.Cache, oid, path, externalURL, now string, contentChanged bool) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	oidFile := cacheOIDEntryFile(cache, oid)
	entry, err := readOIDEntry(oidFile, oid, now)
	if err != nil {
		return err
	}

	pathSet := make(map[string]struct{}, len(entry.Paths)+1)
	for _, p := range entry.Paths {
		pathSet[p] = struct{}{}
	}
	if path != "" {
		pathSet[path] = struct{}{}
	}
	entry.Paths = sortedKeys(pathSet)
	entry.UpdatedAt = now
	entry.ContentChange = entry.ContentChange || contentChanged
	if strings.TrimSpace(externalURL) != "" {
		entry.ExternalURL = externalURL
	}

	return writeJSONAtomic(oidFile, entry)
}

// removePathFromOID removes `path` from the OID entry for `oid` and writes
// the updated entry atomically. Missing entries are treated as empty.
func removePathFromOID(cache *precommit_cache.Cache, oid, path, now string) error {
	if cache == nil {
		return errors.New("cache is nil")
	}
	oidFile := cacheOIDEntryFile(cache, oid)
	entry, err := readOIDEntry(oidFile, oid, now)
	if err != nil {
		return err
	}
	pathSet := make(map[string]struct{}, len(entry.Paths))
	for _, p := range entry.Paths {
		if p == path {
			continue
		}
		pathSet[p] = struct{}{}
	}
	entry.Paths = sortedKeys(pathSet)
	entry.UpdatedAt = now

	return writeJSONAtomic(oidFile, entry)
}

// sortedKeys returns a sorted slice of keys from the provided string-set map.
func sortedKeys(set map[string]struct{}) []string {
	keys := slices.Collect(maps.Keys(set))
	slices.Sort(keys)
	return keys
}
