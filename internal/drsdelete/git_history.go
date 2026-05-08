package drsdelete

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/lfs"
)

type deletedPointer struct {
	Path string
	OID  string
}

func collectDeletedPointers(ctx context.Context, refs []RefUpdate) (map[string][]deletedPointer, error) {
	grouped := make(map[string][]deletedPointer)
	seen := make(map[string]struct{})
	for _, ref := range refs {
		oldSHA := strings.TrimSpace(ref.OldSHA)
		newSHA := strings.TrimSpace(ref.NewSHA)
		if oldSHA == "" || newSHA == "" || isZeroSHA(oldSHA) || isZeroSHA(newSHA) {
			continue
		}
		paths, err := gitDeletedPaths(ctx, oldSHA, newSHA)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			key := oldSHA + "\x00" + path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}

			oid, ok, err := gitPointerOID(ctx, oldSHA, path)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			grouped[oid] = append(grouped[oid], deletedPointer{Path: path, OID: oid})
		}
	}
	return grouped, nil
}

func deletedPaths(items []deletedPointer) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Path)
	}
	sort.Strings(out)
	return out
}

func gitDeletedPaths(ctx context.Context, oldSHA, newSHA string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--name-status", "--diff-filter=D", "-M", oldSHA, newSHA)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff deleted paths %s..%s: %s", oldSHA, newSHA, strings.TrimSpace(string(out)))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	paths := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 || parts[0] != "D" {
			continue
		}
		paths = append(paths, parts[1])
	}
	return paths, nil
}

func gitPointerOID(ctx context.Context, ref, path string) (string, bool, error) {
	spec := ref + ":" + path
	cmd := exec.CommandContext(ctx, "git", "show", spec)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", false, fmt.Errorf("git show %s: %s", spec, strings.TrimSpace(string(out)))
	}
	oid, _, ok := lfs.ParseLFSPointer(out)
	if !ok {
		return "", false, nil
	}
	return "sha256:" + strings.TrimPrefix(strings.TrimSpace(oid), "sha256:"), true, nil
}

func isZeroSHA(sha string) bool {
	return strings.TrimSpace(sha) == "0000000000000000000000000000000000000000"
}
