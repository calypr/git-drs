package precommit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/precommit_cache"
)

func TestRun_NonLFS(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := filepath.Join(repo, "data", "file.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("plain content"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.txt")

	if err := run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1", "paths")
	encoded := precommit_cache.EncodePath("data/file.txt")
	pathEntry := filepath.Join(cacheRoot, encoded+".json")
	if _, err := os.Stat(pathEntry); !os.IsNotExist(err) {
		t.Fatalf("expected no cache entry for non-LFS file, got err=%v", err)
	}
}

func TestRun_LFS(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lfsPointer := strings.Join([]string{
		"version https://git-lfs.github.com/spec/v1",
		"oid sha256:deadbeef",
		"size 12",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(lfsPointer), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")

	// Set time slightly in past to ensure updated comparison works if needed?
	// run() uses time.Now()

	if err := run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	pathsDir := filepath.Join(cacheRoot, "paths")

	encoded := precommit_cache.EncodePath("data/file.bin")
	pathEntry := filepath.Join(pathsDir, encoded+".json")

	pathData, err := os.ReadFile(pathEntry)
	if err != nil {
		t.Fatalf("read path entry: %v", err)
	}
	var pathCache precommit_cache.PathEntry
	if err := json.Unmarshal(pathData, &pathCache); err != nil {
		t.Fatalf("unmarshal path entry: %v", err)
	}
	if pathCache.Path != "data/file.bin" {
		t.Fatalf("expected path entry to be data/file.bin, got %q", pathCache.Path)
	}
	if pathCache.LFSOID != "sha256:deadbeef" {
		t.Fatalf("expected lfs oid sha256:deadbeef, got %q", pathCache.LFSOID)
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test User")
	gitCmd(t, dir, "config", "init.defaultBranch", "main")
	return dir
}

func mustChdir(t *testing.T, dir string) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return old
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	// Wait logic or retry? No, git init is usually fast.
	// Add -c core.safecrlf=false to avoid warnings?
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+dir)
	// mock home to avoid global config interference
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
}
