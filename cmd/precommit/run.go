// Package precommit updates the local `.git/drs/pre-commit` cache from staged
// pointer changes. The cache is rebuildable local bookkeeping, distinct from
// the authoritative local DRS metadata stored under `.git/drs/lfs/objects`.
package precommit

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/calypr/git-drs/internal/precommit_cache"
)

type ChangeKind int

const (
	KindAdd ChangeKind = iota
	KindModify
	KindDelete
	KindRename
)

type Change struct {
	Kind    ChangeKind
	OldPath string
	NewPath string
	Status  string
}

func run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}
	if err := precommit_cache.EnsureLayout(cache); err != nil {
		return err
	}
	tombsDir := filepath.Join(cache.Root, "tombstones")
	if err := os.MkdirAll(tombsDir, 0o755); err != nil {
		return fmt.Errorf("create tombstones directory: %w", err)
	}

	changes, err := stagedChanges(ctx)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	oversized, err := collectOversizedPlainGitStagedFiles(ctx, changes, directCommitWarningThresholdBytes)
	if err != nil {
		return err
	}
	if len(oversized) > 0 {
		allowed, err := confirmOversizedDirectGitCommit(oversized)
		if err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("commit aborted so you can track large files before committing them directly to Git")
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, ch := range changes {
		if ch.Kind != KindRename {
			continue
		}
		newOID, newIsLFS, err := stagedLFSOID(ctx, ch.NewPath)
		if err != nil {
			continue
		}

		oldPathFile := precommit_cache.PathEntryPath(cache, ch.OldPath)
		newPathFile := precommit_cache.PathEntryPath(cache, ch.NewPath)

		if newIsLFS {
			if err := moveFileBestEffort(oldPathFile, newPathFile); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("rename migrate path entry: %w", err)
			}

			if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
				Path:      ch.NewPath,
				LFSOID:    newOID,
				UpdatedAt: now,
			}); err != nil {
				return err
			}

			if err := precommit_cache.UpsertOIDPath(cache, newOID, ch.OldPath, ch.NewPath, "", now, false); err != nil {
				return err
			}
		} else {
			if err := removeIfExists(oldPathFile); err != nil {
				return fmt.Errorf("remove stale path entry for %s: %w", ch.OldPath, err)
			}
		}
	}

	for _, ch := range changes {
		switch ch.Kind {
		case KindAdd, KindModify:
			if err := handleUpsert(ctx, cache, ch.NewPath, now); err != nil {
				return err
			}
		case KindRename:
			if err := handleUpsert(ctx, cache, ch.NewPath, now); err != nil {
				return err
			}
		case KindDelete:
			if err := handleDelete(ctx, cache, tombsDir, ch.NewPath, now); err != nil {
				return err
			}
		}
	}

	return nil
}
