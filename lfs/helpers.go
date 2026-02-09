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

	"github.com/calypr/git-drs/utils"
)

// runGitAllowMissing treats "key not found" as empty output, not an error.
func runGitAllowMissing(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	// Use Output() to get stdout only.
	// GIT_TRACE et al go to stderr.
	b, err := cmd.Output()
	//if err != nil {
	//	// "git config --get missing.key" exits 1 with empty output.
	//	var exitErr *exec.ExitError
	//	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
	//		return "", nil
	//	}
	//}
	if err != nil {
		// "git config --get missing.key" exits 1 with empty output.
		s := strings.TrimSpace(string(b))
		if s == "" {
			return "", nil
		}
		return "", fmt.Errorf("%v: >%s<", err, s)
	}
	return string(b), nil
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
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
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
	out, err := exec.Command("sh", "-lc", "printf %s \"$HOME\"").Output()
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
