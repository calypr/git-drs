package lfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/calypr/git-drs/utils"
)

// runGitAllowMissing treats "key not found" as empty output, not an error.
func runGitAllowMissing(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		// "git config --get missing.key" exits 1 with empty output.
		s := strings.TrimSpace(stdout.String())
		if s == "" {
			return "", nil
		}
		// If stdout is not empty, it might be an actual value or a real error.
		// However, for --get, if it failed, we usually care about stderr for the error message.
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), errMsg)
	}
	return stdout.String(), nil
}

// resolveLFSRoot implements:
// - if `git config --get lfs.storage` is set: use it
//   - if relative: resolve relative to GitCommonDir (this is how git-lfs treats it in practice)
//
// - else: <GitCommonDir>/lfs
func resolveLFSRoot(ctx context.Context, gitCommonDir string) (string, error) {
	// NOTE: git config --get returns exit status 1 if key not found.
	out, err := runGitAllowMissing(ctx, "config", "--get", "lfs.storage")
	if err != nil {
		return "", fmt.Errorf("git config --get lfs.storage failed: %w", err)
	}
	val := strings.TrimSpace(out)

	if val == "" {
		return filepath.Clean(filepath.Join(gitCommonDir, "lfs")), nil
	}

	// Expand ~ if present (nice-to-have).
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
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), errMsg)
	}
	return stdout.String(), nil
}

func userHomeDir() (string, error) {
	// Avoid os/user on some cross-compile scenarios; keep it simple.
	if runtime.GOOS == "windows" {
		// Not your target, but safe fallback.
		return "", errors.New("home expansion not supported on windows in this helper")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return home, nil
	}
	// macOS/Linux
	out, err := exec.Command("sh", "-lc", "printf %s \"$HOME\"").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func GetGitAttribute(ctx context.Context, attr string, path string) (string, error) {
	out, err := runGit(ctx, "check-attr", attr, "--", path)
	if err != nil {
		return "", fmt.Errorf("git check-attr failed: %w", err)
	}
	return out, nil
}

//
// --- Git helpers ---
//

func GitLFSTrack(ctx context.Context, path string) (bool, error) {
	out, err := runGit(ctx, "lfs", "track", path)
	if err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}
	return strings.Contains(out, path), nil
}

func GitLFSTrackReadOnly(ctx context.Context, path string) (bool, error) {
	_, err := GitLFSTrack(ctx, path)
	if err != nil {
		return false, fmt.Errorf("git lfs track failed: %w", err)
	}

	repoRoot, err := utils.GitTopLevel()
	if err != nil {
		return false, err
	}

	attrPath := filepath.Join(repoRoot, ".gitattributes")
	changed, err := UpsertDRSRouteLines(attrPath, "ro", []string{path})
	if err != nil {
		return false, err
	}

	return changed, nil
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
	// If relative, resolve it against current working directory.
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err == nil {
			dir = abs
		}
	}
	return dir, nil
}

// GetGitRootDirectories
// returns (gitCommonDir, lfsRoot, error).
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
