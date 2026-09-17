package precommit

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/calypr/git-drs/internal/precommit_cache"
)

func handleUpsert(ctx context.Context, cache *precommit_cache.Cache, path, now string) error {
	oid, isLFS, err := stagedLFSOID(ctx, path)
	if err != nil {
		return nil
	}
	if !isLFS {
		return nil
	}

	prev, prevExists, err := precommit_cache.ReadPathEntry(cache, path)
	if err != nil {
		return err
	}

	if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
		Path:      path,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	contentChanged := prevExists && prev != nil && prev.LFSOID != oid
	if err := precommit_cache.UpsertOIDPath(cache, oid, "", path, "", now, contentChanged); err != nil {
		return err
	}
	if contentChanged {
		if err := precommit_cache.RemoveOIDPath(cache, prev.LFSOID, path, now); err != nil {
			return fmt.Errorf("remove stale OID path mapping for %s: %w", path, err)
		}
	}

	return nil
}

func handleDelete(cache *precommit_cache.Cache, tombsDir, path, now string) error {
	entry, ok, err := precommit_cache.ReadPathEntry(cache, path)
	if err != nil || !ok {
		return nil
	}
	if err := removeIfExists(precommit_cache.PathEntryPath(cache, path)); err != nil {
		return fmt.Errorf("remove path entry for %s: %w", path, err)
	}
	if entry.LFSOID != "" {
		if err := precommit_cache.RemoveOIDPath(cache, entry.LFSOID, path, now); err != nil {
			return fmt.Errorf("remove OID path mapping for %s: %w", path, err)
		}
	}

	tombFile := filepath.Join(tombsDir, precommit_cache.EncodePath(path)+".json")
	if err := precommit_cache.WriteJSONAtomic(tombFile, map[string]string{
		"path":       path,
		"deleted_at": now,
	}); err != nil {
		return fmt.Errorf("write tombstone for %s: %w", path, err)
	}

	return nil
}
