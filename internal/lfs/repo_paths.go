package lfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runGitAllowMissing treats "key not found" as empty output, not an error.
func runGitAllowMissing(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.Output()
	if err != nil {
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", nil
		}
		return "", fmt.Errorf("%v: >%s<", err, s)
	}
	return string(b), nil
}

// resolveLFSRoot mirrors git-lfs storage resolution:
// - if lfs.storage is set, use it relative to GitCommonDir when needed
// - otherwise use <GitCommonDir>/lfs
func resolveLFSRoot(ctx context.Context, gitCommonDir string) (string, error) {
	out, err := runGitAllowMissing(ctx, "config", "--get", "lfs.storage")
	if err != nil {
		return "", fmt.Errorf("git config --get lfs.storage failed: %w", err)
	}
	val := strings.TrimSpace(out)

	if val == "" {
		return filepath.Clean(filepath.Join(gitCommonDir, "lfs")), nil
	}

	if strings.HasPrefix(val, "~") && (len(val) == 1 || val[1] == '/' || val[1] == '\\') {
		home, herr := userHomeDir()
		if herr == nil && home != "" {
			val = filepath.Join(home, strings.TrimPrefix(val, "~"))
		}
	}

	if !filepath.IsAbs(val) {
		val = filepath.Join(gitCommonDir, val)
	}
	return filepath.Clean(val), nil
}

func runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

func userHomeDir() (string, error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("home expansion not supported on windows in this helper")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	out, err := exec.Command("sh", "-lc", "printf %s \"$HOME\"").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func gitRevParseGitCommonDir(ctx context.Context) (string, error) {
	out, err := runGit(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir failed: %w", err)
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", errors.New("git rev-parse returned empty --git-common-dir")
	}
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
	}
	return dir, nil
}

// GetGitRootDirectories returns the Git common directory and LFS object root.
func GetGitRootDirectories(ctx context.Context) (string, string, error) {
	gitCommonDir, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		return "", "", err
	}
	lfsRoot, err := resolveLFSRoot(ctx, gitCommonDir)
	if err != nil {
		return "", "", err
	}
	if lfsRoot == "" {
		lfsRoot = filepath.Join(gitCommonDir, "lfs")
	}
	return gitCommonDir, lfsRoot, nil
}
