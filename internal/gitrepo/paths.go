package gitrepo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	LFSObjectsPath = ".git/lfs/objects"
	DRSObjectsPath = ".git/drs/lfs/objects"
	ConfigYAML     = "config.yaml"
	RepoDRSDir     = ".git-drs"
	DRSLogFile     = ".git/drs/git-drs.log"
	DRSDir         = ".git/drs"
)

// SafeWorktreePath validates a repository-relative path and rejects symlinked
// components before a caller writes through it. Callers should re-check the
// returned path immediately before opening it when the filesystem is mutable.
func SafeWorktreePath(root, relative string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("worktree root is empty")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve worktree root: %w", err)
	}
	if relative == "" {
		return "", fmt.Errorf("worktree path is empty")
	}
	relative = filepath.FromSlash(relative)
	clean := filepath.Clean(relative)
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("worktree path %q must stay inside repository", relative)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("worktree path %q escapes repository", relative)
	}
	if err := rejectSymlinkComponents(root, rel); err != nil {
		return "", err
	}
	return target, nil
}

func rejectSymlinkComponents(root, relative string) error {
	current := root
	if info, err := os.Lstat(current); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("worktree root is a symlink")
	}
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("inspect worktree path %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("worktree path %q contains symlink component", relative)
		}
	}
	return nil
}
