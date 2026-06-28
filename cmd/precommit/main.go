// Package precommit updates the local `.git/drs/pre-commit` cache from staged
// pointer changes. The cache is rebuildable local bookkeeping, distinct from
// the authoritative local DRS metadata stored under `.git/drs/lfs/objects`.
package precommit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/calypr/git-drs/internal/precommit_cache"
	"github.com/spf13/cobra"
)

const (
	lfsSpecLine                         = "version https://git-lfs.github.com/spec/v1"
	defaultDirectCommitWarningThreshold = int64(10 * 1024 * 1024)
)

var (
	directCommitWarningThresholdBytes = defaultDirectCommitWarningThreshold
	confirmOversizedDirectGitCommit   = promptOversizedDirectGitCommit
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
	OldPath string // for rename
	NewPath string // for rename (and for add/modify/delete uses NewPath)
	Status  string // raw status, e.g. "A", "M", "D", "R100"
}

type OversizedStagedFile struct {
	Path string
	Size int64
}

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "precommit",
	Short: "pre-commit hook to update local DRS cache",
	Long:  "Pre-commit hook that updates the local DRS pre-commit cache",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return run(context.Background())
	},
}

func run(ctx context.Context) error {
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}
	if err := precommit_cache.EnsureLayout(cache); err != nil {
		return err
	}
	tombsDir := filepath.Join(cache.Root, "tombstones")
	_ = os.MkdirAll(tombsDir, 0o755) // optional

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

	// Process renames first so subsequent add/modify logic sees the "new" path.
	// This mirrors how we want cache paths to follow staged paths.
	for _, ch := range changes {
		if ch.Kind != KindRename {
			continue
		}
		// Only act if BOTH old and new are LFS in scope? Prefer:
		// - If the new path is LFS, we migrate.
		// - If it isn't LFS, we remove old path entry (out of scope).
		newOID, newIsLFS, err := stagedLFSOID(ctx, ch.NewPath)
		if err != nil {
			// If file doesn't exist in index due to weird staging, skip.
			continue
		}

		oldPathFile := precommit_cache.PathEntryPath(cache, ch.OldPath)
		newPathFile := precommit_cache.PathEntryPath(cache, ch.NewPath)

		if newIsLFS {
			// Move/overwrite path entry file
			if err := moveFileBestEffort(oldPathFile, newPathFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("rename migrate path entry: %w", err)
			}

			// Ensure path entry content correct
			if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
				Path:      ch.NewPath,
				LFSOID:    newOID,
				UpdatedAt: now,
			}); err != nil {
				return err
			}

			// Update oid entry: replace old path with new path for that OID
			if err := precommit_cache.UpsertOIDPath(cache, newOID, ch.OldPath, ch.NewPath, "", now, false); err != nil {
				return err
			}
		} else {
			// Out of scope now: remove any cached path entry.
			_ = os.Remove(oldPathFile)
		}
	}

	// Process adds/modifies/deletes (and renames again just to ensure content correctness on new path).
	for _, ch := range changes {
		switch ch.Kind {
		case KindAdd, KindModify:
			if err := handleUpsert(ctx, cache, ch.NewPath, now); err != nil {
				return err
			}
		case KindRename:
			// Treat like upsert on NewPath to ensure OID/path consistency if content also changed.
			if err := handleUpsert(ctx, cache, ch.NewPath, now); err != nil {
				return err
			}
			// Optionally also remove old path from *other* OID entry if rename+content-change changed OID.
			// We'll do it inside handleUpsert by checking previous cached OID for that path (after move).
		case KindDelete:
			if err := handleDelete(ctx, cache, tombsDir, ch.NewPath, now); err != nil {
				return err
			}
		}
	}

	return nil
}

func handleUpsert(ctx context.Context, cache *precommit_cache.Cache, path, now string) error {
	oid, isLFS, err := stagedLFSOID(ctx, path)
	if err != nil {
		// If file isn't in index, ignore.
		return nil
	}
	if !isLFS {
		// Out of scope.
		return nil
	}

	prev, prevExists, err := precommit_cache.ReadPathEntry(cache, path)
	if err != nil {
		return err
	}

	// Write/update path entry.
	if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
		Path:      path,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	// Update OID entry for new oid: add path.
	contentChanged := prevExists && prev != nil && prev.LFSOID != oid
	if err := precommit_cache.UpsertOIDPath(cache, oid, "", path, "", now, contentChanged); err != nil {
		return err
	}

	// If content changed, remove path from the *old* oid entry (best effort).
	if contentChanged {
		_ = precommit_cache.RemoveOIDPath(cache, prev.LFSOID, path, now)
	}

	return nil
}

func handleDelete(ctx context.Context, cache *precommit_cache.Cache, tombsDir, path, now string) error {
	// Only consider deletion if it was previously an LFS entry (cache-driven).
	entry, ok, err := precommit_cache.ReadPathEntry(cache, path)
	if err != nil || !ok {
		// nothing to do
		return nil
	}
	// Remove path entry.
	_ = os.Remove(precommit_cache.PathEntryPath(cache, path))

	// Remove this path from the old oid entry (best effort).
	if entry.LFSOID != "" {
		_ = precommit_cache.RemoveOIDPath(cache, entry.LFSOID, path, now)
	}

	// Optional tombstone.
	tombFile := filepath.Join(tombsDir, precommit_cache.EncodePath(path)+".json")
	_ = writeJSONAtomic(tombFile, map[string]string{
		"path":       path,
		"deleted_at": now,
	})

	return nil
}

// stagedChanges parses: git diff --cached --name-status -M
// Formats:
//
//	A<TAB>path
//	M<TAB>path
//	D<TAB>path
//	R100<TAB>old<TAB>new
func stagedChanges(ctx context.Context) ([]Change, error) {
	out, err := git(ctx, "diff", "--cached", "--name-status", "-M")
	if err != nil {
		return nil, err
	}
	var changes []Change
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		status := parts[0]
		switch {
		case status == "A":
			changes = append(changes, Change{Kind: KindAdd, NewPath: parts[1], Status: status})
		case status == "M":
			changes = append(changes, Change{Kind: KindModify, NewPath: parts[1], Status: status})
		case status == "D":
			changes = append(changes, Change{Kind: KindDelete, NewPath: parts[1], Status: status})
		case strings.HasPrefix(status, "R") && len(parts) >= 3:
			changes = append(changes, Change{Kind: KindRename, OldPath: parts[1], NewPath: parts[2], Status: status})
		default:
			// ignore other statuses (C, T, U, etc) for this reference impl
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

// stagedLFSOID returns (oid, isLFS, err) based on STAGED content.
// isLFS is true only if the staged file is a valid LFS pointer with an oid sha256 line.
func stagedLFSOID(ctx context.Context, path string) (string, bool, error) {
	out, err := git(ctx, "show", ":"+path)
	if err != nil {
		// path may not exist in index (deleted/intent-to-add weirdness)
		return "", false, err
	}

	// Fast parse: look for spec line and oid line near top.
	// LFS pointer files are small; scanning full content is fine.
	var hasSpec bool
	var oid string

	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if line == lfsSpecLine {
			hasSpec = true
			continue
		}
		if strings.HasPrefix(line, "oid sha256:") {
			hex := strings.TrimPrefix(line, "oid sha256:")
			hex = strings.TrimSpace(hex)
			if hex != "" {
				oid = "sha256:" + hex
			}
			// keep scanning a bit in case spec is below (rare), but we can break once both are found.
		}
		// pointer usually has only a few lines; stop early after 10 lines
		if hasSpec && oid != "" {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, err
	}

	if hasSpec && oid != "" {
		return oid, true, nil
	}
	return "", false, nil
}

func stagedBlobSize(ctx context.Context, path string) (int64, error) {
	out, err := git(ctx, "cat-file", "-s", ":"+path)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse staged blob size for %s: %w", path, err)
	}
	return size, nil
}

func collectOversizedPlainGitStagedFiles(ctx context.Context, changes []Change, thresholdBytes int64) ([]OversizedStagedFile, error) {
	if thresholdBytes <= 0 {
		return nil, nil
	}
	var oversized []OversizedStagedFile
	seen := make(map[string]struct{})
	for _, ch := range changes {
		if ch.Kind != KindAdd && ch.Kind != KindModify && ch.Kind != KindRename {
			continue
		}
		path := ch.NewPath
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		_, isLFS, err := stagedLFSOID(ctx, path)
		if err != nil {
			continue
		}
		if isLFS {
			continue
		}

		size, err := stagedBlobSize(ctx, path)
		if err != nil {
			return nil, err
		}
		if size <= thresholdBytes {
			continue
		}
		oversized = append(oversized, OversizedStagedFile{Path: path, Size: size})
	}
	sort.Slice(oversized, func(i, j int) bool { return oversized[i].Path < oversized[j].Path })
	return oversized, nil
}

func promptOversizedDirectGitCommit(files []OversizedStagedFile) (bool, error) {
	if len(files) == 0 {
		return true, nil
	}

	fmt.Fprintf(os.Stderr, "\nWarning: the following staged files are being committed directly to Git and exceed %s:\n\n", humanBytes(directCommitWarningThresholdBytes))
	for _, f := range files {
		fmt.Fprintf(os.Stderr, "  - %s (%s)\n", f.Path, humanBytes(f.Size))
	}
	fmt.Fprintln(os.Stderr, "\nIf these should be managed by git-drs, track them first and re-add them.")
	fmt.Fprint(os.Stderr, "Continue committing these files directly to GitHub? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func humanBytes(n int64) string {
	const unit = int64(1024)
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := unit, 0
	for q := n / unit; q >= unit; q /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func git(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		// include stderr for debugging; don’t leak massive output
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return stdout.Bytes(), nil
}

// writeJSONAtomic writes JSON to a temp file then renames it into place.
// This avoids partially written cache files if the process is interrupted.
func writeJSONAtomic(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func moveFileBestEffort(src, dst string) error {
	// Ensure destination directory exists.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Rename will fail across devices; fall back to copy+remove.
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
