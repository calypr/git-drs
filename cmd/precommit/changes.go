package precommit

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/lfs"
)

// stagedChanges parses NUL-delimited Git paths without changing filename bytes.
func stagedChanges(ctx context.Context) ([]Change, error) {
	out, err := git(ctx, "diff", "--cached", "--name-status", "-z", "-M")
	if err != nil {
		return nil, err
	}
	var changes []Change
	fields := bytes.Split(out, []byte{0})
	for i := 0; i < len(fields)-1; {
		status := string(fields[i])
		i++
		if i >= len(fields)-1 {
			return nil, fmt.Errorf("incomplete staged change for status %q", status)
		}
		path := string(fields[i])
		i++
		switch {
		case status == "A":
			changes = append(changes, Change{Kind: KindAdd, NewPath: path})
		case status == "M" || status == "T":
			changes = append(changes, Change{Kind: KindModify, NewPath: path})
		case status == "D":
			changes = append(changes, Change{Kind: KindDelete, NewPath: path})
		case strings.HasPrefix(status, "R"):
			if i >= len(fields)-1 {
				return nil, fmt.Errorf("incomplete staged rename for %q", path)
			}
			changes = append(changes, Change{Kind: KindRename, OldPath: path, NewPath: string(fields[i])})
			i++
		}
	}
	return changes, nil
}

func stagedLFSOID(ctx context.Context, path string) (string, bool, error) {
	size, err := stagedBlobSize(ctx, path)
	if err != nil {
		return "", false, err
	}
	if size > lfs.MaxPointerFileBytes {
		return "", false, nil
	}
	out, err := git(ctx, "show", stagedObjectSpec(path))
	if err != nil {
		return "", false, err
	}
	var hasSpec bool
	var oid string
	for _, raw := range bytes.Split(out, []byte{'\n'}) {
		line := string(bytes.TrimSuffix(raw, []byte{'\r'}))
		if line == "version https://git-lfs.github.com/spec/v1" {
			hasSpec = true
		}
		if strings.HasPrefix(line, "oid sha256:") {
			hex := strings.TrimSpace(strings.TrimPrefix(line, "oid sha256:"))
			if hex != "" {
				oid = "sha256:" + hex
			}
		}
		if hasSpec && oid != "" {
			return oid, true, nil
		}
	}
	return "", false, nil
}

func stagedBlobSize(ctx context.Context, path string) (int64, error) {
	out, err := git(ctx, "cat-file", "-s", stagedObjectSpec(path))
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse staged blob size for %s: %w", path, err)
	}
	return size, nil
}

// The ./ prefix prevents a filename such as 0:big.bin from being parsed as
// Git's :<stage>:<path> index selector.
func stagedObjectSpec(path string) string {
	return ":./" + path
}
