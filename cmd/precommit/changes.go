package precommit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// stagedChanges parses: git diff --cached --name-status -M
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
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return changes, nil
}

func stagedLFSOID(ctx context.Context, path string) (string, bool, error) {
	out, err := git(ctx, "show", ":"+path)
	if err != nil {
		return "", false, err
	}

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
			hex := strings.TrimSpace(strings.TrimPrefix(line, "oid sha256:"))
			if hex != "" {
				oid = "sha256:" + hex
			}
		}
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
