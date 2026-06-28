package addurl

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/precommit_cache"
)

// updatePrecommitCache updates the project's pre-commit cache with a mapping
// from a repository-relative `pathArg` to the given LFS `oid` and records the
// external source URL.
func updatePrecommitCache(ctx context.Context, logger *slog.Logger, pathArg, oid, externalURL string) error {
	if logger == nil {
		return errors.New("logger is required")
	}
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}
	if err := ensureCacheDirs(cache, logger); err != nil {
		return err
	}

	relPath, err := repoRelativePath(pathArg)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	prevEntry, prevExists, err := precommit_cache.ReadPathEntry(cache, relPath)
	if err != nil {
		return err
	}
	contentChanged := prevExists && prevEntry.LFSOID != "" && prevEntry.LFSOID != oid

	if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
		Path:      relPath,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}
	if err := precommit_cache.UpsertOIDPath(cache, oid, "", relPath, externalURL, now, contentChanged); err != nil {
		return err
	}
	if contentChanged {
		if err := precommit_cache.RemoveOIDPath(cache, prevEntry.LFSOID, relPath, now); err != nil {
			return fmt.Errorf("remove stale OID path mapping for %s: %w", relPath, err)
		}
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
	if err := precommit_cache.EnsureLayout(cache); err != nil {
		return fmt.Errorf("create cache layout: %w", err)
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
