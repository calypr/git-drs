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
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/calypr/git-drs/precommit_cache"
	"github.com/spf13/cobra"
)

type ChangeKind int

const (
	KindAdd ChangeKind = iota
	KindModify
	KindDelete
	KindRename
	lfsSpecLine = "version https://git-lfs.github.com/spec/v1"
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
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}

	changes, err := stagedChanges(ctx)
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}

	// Process renames first so subsequent add/modify logic sees the "new" path.
	for _, ch := range changes {
		if ch.Kind != KindRename {
			continue
		}
		newOID, newIsLFS, err := stagedLFSOID(ctx, ch.NewPath)
		if err != nil {
			continue
		}

		if newIsLFS {
			// Delete old entry
			_ = cache.DeletePathEntry(ctx, logger, ch.OldPath)
			// Add new entry
			if err := cache.UpdatePathEntry(ctx, logger, ch.NewPath, newOID, ""); err != nil {
				return err
			}
		} else {
			// Out of scope now: remove any cached path entry for old path.
			_ = cache.DeletePathEntry(ctx, logger, ch.OldPath)
		}
	}

	// Process adds/modifies/deletes
	for _, ch := range changes {
		switch ch.Kind {
		case KindAdd, KindModify:
			oid, isLFS, err := stagedLFSOID(ctx, ch.NewPath)
			if err != nil {
				continue // ignore
			}
			if !isLFS {
				continue // ignore
			}
			if err := cache.UpdatePathEntry(ctx, logger, ch.NewPath, oid, ""); err != nil {
				return err
			}
		case KindRename:
			// Handled in first loop.
		case KindDelete:
			_ = cache.DeletePathEntry(ctx, logger, ch.NewPath)
		}
	}

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
