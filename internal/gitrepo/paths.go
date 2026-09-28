package gitrepo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

// RepositoryPaths separates paths that belong to one worktree from paths
// shared by every worktree in the same repository.
type RepositoryPaths struct {
	GitDir    string
	CommonDir string
	HooksDir  string
}

// ResolveRepositoryPaths asks Git for the active worktree's metadata paths.
// The hooks path is resolved by Git so core.hooksPath is honored.
func ResolveRepositoryPaths(ctx context.Context) (RepositoryPaths, error) {
	gitDir, err := resolveGitPath(ctx, "--git-dir")
	if err != nil {
		return RepositoryPaths{}, fmt.Errorf("resolve Git directory: %w", err)
	}
	commonDir, err := resolveGitPath(ctx, "--git-common-dir")
	if err != nil {
		return RepositoryPaths{}, fmt.Errorf("resolve Git common directory: %w", err)
	}
	hooksDir, err := resolveGitPath(ctx, "--git-path", "hooks")
	if err != nil {
		return RepositoryPaths{}, fmt.Errorf("resolve Git hooks path: %w", err)
	}
	return RepositoryPaths{GitDir: gitDir, CommonDir: commonDir, HooksDir: hooksDir}, nil
}

func ResolveDRSDir(ctx context.Context) (string, error) {
	commonDir, err := ResolveGitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "drs"), nil
}

func ResolveDRSObjectsDir(ctx context.Context) (string, error) {
	commonDir, err := ResolveGitCommonDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(commonDir, "drs", "lfs", "objects"), nil
}

// ResolveGitCommonDir returns the shared Git metadata directory. This is the
// storage anchor shared by linked worktrees.
func ResolveGitCommonDir(ctx context.Context) (string, error) {
	return resolveGitPath(ctx, "--git-common-dir")
}

func resolveGitPath(ctx context.Context, args ...string) (string, error) {
	cmdArgs := append([]string{"rev-parse", "--path-format=absolute"}, args...)
	cmd := exec.CommandContext(ctx, "git", cmdArgs...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(string(output), "\n")
	if value == "" || !filepath.IsAbs(value) {
		return "", fmt.Errorf("Git returned invalid path %q", value)
	}
	return filepath.Clean(value), nil
}

// DRSDir returns the shared directory for repository metadata.
func (p RepositoryPaths) DRSDir() string {
	return filepath.Join(p.CommonDir, "drs")
}

// DRSObjectsDir returns the shared directory for local DRS object records.
func (p RepositoryPaths) DRSObjectsDir() string {
	return filepath.Join(p.DRSDir(), "lfs", "objects")
}

// DRSLogFile returns the log path scoped to the active worktree.
func (p RepositoryPaths) DRSLogFile() string {
	return filepath.Join(p.GitDir, "drs", "git-drs.log")
}

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
