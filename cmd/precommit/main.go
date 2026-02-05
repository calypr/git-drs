// Package precommit
// -------------------------------------
// LFS-only local cache updater for:
//   - Path -> OID  : .git/drs/pre-commit/v1/paths/<encoded-path>.json
//   - OID  -> Paths + S3 URL hint : .git/drs/pre-commit/v1/oids/<oid>.json
//
// This hook is intentionally:
//   - LFS-only (non-LFS paths are ignored)
//   - local-only (no network, no server index reads)
//   - index-based (reads STAGED content via `git show :<path>`)
//
// Note: This is a reference implementation. Adjust logging/policy as desired.
package precommit

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	cacheVersionDir = "drs/pre-commit/v1"
	lfsSpecLine     = "version https://git-lfs.github.com/spec/v1"
)

type PathEntry struct {
	Path      string `json:"path"`
	LFSOID    string `json:"lfs_oid"`
	UpdatedAt string `json:"updated_at"`
}

type OIDEntry struct {
	LFSOID        string   `json:"lfs_oid"`
	Paths         []string `json:"paths"`
	S3URL         string   `json:"s3_url,omitempty"` // hint only; may be empty
	UpdatedAt     string   `json:"updated_at"`
	ContentChange bool     `json:"content_changed"`
}

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

func main() {
	ctx := context.Background()
	if err := run(ctx); err != nil {
		// For a reference impl, treat errors as non-fatal unless you want strict enforcement.
		// Exiting non-zero blocks the commit.
		fmt.Fprintf(os.Stderr, "pre-commit drs cache: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	gitDir, err := gitRevParseGitDir(ctx)
	if err != nil {
		return err
	}

	cacheRoot := filepath.Join(gitDir, cacheVersionDir)
	pathsDir := filepath.Join(cacheRoot, "paths")
	oidsDir := filepath.Join(cacheRoot, "oids")
	tombsDir := filepath.Join(cacheRoot, "tombstones")

	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(oidsDir, 0o755); err != nil {
		return err
	}
	_ = os.MkdirAll(tombsDir, 0o755) // optional

	changes, err := stagedChanges(ctx)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
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

		oldPathFile := pathEntryFile(pathsDir, ch.OldPath)
		newPathFile := pathEntryFile(pathsDir, ch.NewPath)

		if newIsLFS {
			// Move/overwrite path entry file
			if err := moveFileBestEffort(oldPathFile, newPathFile); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("rename migrate path entry: %w", err)
			}

			// Ensure path entry content correct
			if err := writeJSONAtomic(newPathFile, PathEntry{
				Path:      ch.NewPath,
				LFSOID:    newOID,
				UpdatedAt: now,
			}); err != nil {
				return err
			}

			// Update oid entry: replace old path with new path for that OID
			if err := oidAddOrReplacePath(oidsDir, newOID, ch.OldPath, ch.NewPath, now, false); err != nil {
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
			if err := handleUpsert(ctx, pathsDir, oidsDir, ch.NewPath, now); err != nil {
				return err
			}
		case KindRename:
			// Treat like upsert on NewPath to ensure OID/path consistency if content also changed.
			if err := handleUpsert(ctx, pathsDir, oidsDir, ch.NewPath, now); err != nil {
				return err
			}
			// Optionally also remove old path from *other* OID entry if rename+content-change changed OID.
			// We'll do it inside handleUpsert by checking previous cached OID for that path (after move).
		case KindDelete:
			if err := handleDelete(ctx, pathsDir, oidsDir, tombsDir, ch.NewPath, now); err != nil {
				return err
			}
		}
	}

	return nil
}

func handleUpsert(ctx context.Context, pathsDir, oidsDir, path, now string) error {
	oid, isLFS, err := stagedLFSOID(ctx, path)
	if err != nil {
		// If file isn't in index, ignore.
		return nil
	}
	if !isLFS {
		// Out of scope.
		return nil
	}

	pathFile := pathEntryFile(pathsDir, path)

	// Load previous path entry if it exists to detect content changes.
	var prev PathEntry
	prevExists := false
	if b, err := os.ReadFile(pathFile); err == nil {
		_ = json.Unmarshal(b, &prev)
		if prev.Path != "" && prev.LFSOID != "" {
			prevExists = true
		}
	}

	// Write/update path entry.
	if err := writeJSONAtomic(pathFile, PathEntry{
		Path:      path,
		LFSOID:    oid,
		UpdatedAt: now,
	}); err != nil {
		return err
	}

	// Update OID entry for new oid: add path.
	contentChanged := prevExists && prev.LFSOID != oid
	if err := oidAddOrReplacePath(oidsDir, oid, "", path, now, contentChanged); err != nil {
		return err
	}

	// If content changed, remove path from the *old* oid entry (best effort).
	if contentChanged {
		_ = oidRemovePath(oidsDir, prev.LFSOID, path, now)
	}

	return nil
}

func handleDelete(ctx context.Context, pathsDir, oidsDir, tombsDir, path, now string) error {
	// Only consider deletion if it was previously an LFS entry (cache-driven).
	pathFile := pathEntryFile(pathsDir, path)
	b, err := os.ReadFile(pathFile)
	if err != nil {
		// nothing to do
		return nil
	}
	var pe PathEntry
	if err := json.Unmarshal(b, &pe); err != nil {
		// corrupted cache; remove it
		_ = os.Remove(pathFile)
		return nil
	}
	// Remove path entry.
	_ = os.Remove(pathFile)

	// Remove this path from the old oid entry (best effort).
	if pe.LFSOID != "" {
		_ = oidRemovePath(oidsDir, pe.LFSOID, path, now)
	}

	// Optional tombstone.
	tombFile := filepath.Join(tombsDir, encodePath(path)+".json")
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

func gitRevParseGitDir(ctx context.Context) (string, error) {
	out, err := git(ctx, "rev-parse", "--git-dir")
	if err != nil {
		return "", err
	}
	gitDir := strings.TrimSpace(string(out))
	if gitDir == "" {
		return "", errors.New("could not determine .git dir")
	}
	// If gitDir is relative, resolve relative to repo root
	if !filepath.IsAbs(gitDir) {
		rootOut, err := git(ctx, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", err
		}
		root := strings.TrimSpace(string(rootOut))
		gitDir = filepath.Join(root, gitDir)
	}
	return gitDir, nil
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

// pathEntryFile maps a repo-relative path to a cache file location.
// We keep a deterministic encoding so any path maps to exactly one file.
func pathEntryFile(pathsDir, path string) string {
	return filepath.Join(pathsDir, encodePath(path)+".json")
}

func encodePath(path string) string {
	// base64url encoding of the UTF-8 path string (no padding) is simple and safe.
	return base64.RawURLEncoding.EncodeToString([]byte(path))
}

func oidEntryFile(oidsDir, oid string) string {
	// OID contains ":"; make it filesystem safe but still human readable.
	// Use a stable transform; here: sha256 of oid string to avoid path length issues.
	sum := sha256.Sum256([]byte(oid))
	return filepath.Join(oidsDir, fmt.Sprintf("%x.json", sum[:]))
}

// oidAddOrReplacePath:
// - loads oid entry (if exists)
// - adds newPath to paths[]
// - if oldPath != "" and present, replaces it with newPath
// - sets ContentChange flag if requested (ORed into existing flag)
// - preserves existing s3_url hint
func oidAddOrReplacePath(oidsDir, oid, oldPath, newPath, now string, contentChanged bool) error {
	f := oidEntryFile(oidsDir, oid)

	entry := OIDEntry{
		LFSOID:    oid,
		Paths:     []string{},
		UpdatedAt: now,
	}
	if b, err := os.ReadFile(f); err == nil {
		_ = json.Unmarshal(b, &entry)
		// ensure oid is set even if old file was incomplete
		entry.LFSOID = oid
	}

	paths := make(map[string]struct{}, len(entry.Paths)+1)
	for _, p := range entry.Paths {
		paths[p] = struct{}{}
	}

	if oldPath != "" {
		delete(paths, oldPath)
	}
	if newPath != "" {
		paths[newPath] = struct{}{}
	}

	entry.Paths = keysSorted(paths)
	entry.UpdatedAt = now
	entry.ContentChange = entry.ContentChange || contentChanged

	return writeJSONAtomic(f, entry)
}

func oidRemovePath(oidsDir, oid, path, now string) error {
	f := oidEntryFile(oidsDir, oid)

	b, err := os.ReadFile(f)
	if err != nil {
		return err
	}
	var entry OIDEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return err
	}
	paths := make(map[string]struct{}, len(entry.Paths))
	for _, p := range entry.Paths {
		if p == path {
			continue
		}
		paths[p] = struct{}{}
	}
	entry.Paths = keysSorted(paths)
	entry.UpdatedAt = now

	// If no paths remain, keep the file (it may still hold s3_url hint) or delete it.
	// This ADR allows stale entries; keeping is fine. Optionally delete when empty:
	// if len(entry.Paths) == 0 && entry.S3URL == "" { return os.Remove(f) }

	return writeJSONAtomic(f, entry)
}

func keysSorted(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
