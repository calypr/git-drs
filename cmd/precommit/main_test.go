package precommit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleUpsertIgnoresNonLFSFile(t *testing.T) {
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

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	pathsDir := filepath.Join(cacheRoot, "paths")
	oidsDir := filepath.Join(cacheRoot, "oids")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths: %v", err)
	}
	if err := os.MkdirAll(oidsDir, 0o755); err != nil {
		t.Fatalf("mkdir oids: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := handleUpsert(context.Background(), pathsDir, oidsDir, "data/file.txt", now); err != nil {
		t.Fatalf("handleUpsert: %v", err)
	}

	pathEntry := pathEntryFile(pathsDir, "data/file.txt")
	if _, err := os.Stat(pathEntry); !os.IsNotExist(err) {
		t.Fatalf("expected no cache entry for non-LFS file, got err=%v", err)
	}
}

func TestHandleUpsertWritesLFSPointerCache(t *testing.T) {
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

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	pathsDir := filepath.Join(cacheRoot, "paths")
	oidsDir := filepath.Join(cacheRoot, "oids")
	if err := os.MkdirAll(pathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths: %v", err)
	}
	if err := os.MkdirAll(oidsDir, 0o755); err != nil {
		t.Fatalf("mkdir oids: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if err := handleUpsert(context.Background(), pathsDir, oidsDir, "data/file.bin", now); err != nil {
		t.Fatalf("handleUpsert: %v", err)
	}

	pathEntry := pathEntryFile(pathsDir, "data/file.bin")
	pathData, err := os.ReadFile(pathEntry)
	if err != nil {
		t.Fatalf("read path entry: %v", err)
	}
	var pathCache PathEntry
	if err := json.Unmarshal(pathData, &pathCache); err != nil {
		t.Fatalf("unmarshal path entry: %v", err)
	}
	if pathCache.Path != "data/file.bin" {
		t.Fatalf("expected path entry to be data/file.bin, got %q", pathCache.Path)
	}
	if pathCache.LFSOID != "sha256:deadbeef" {
		t.Fatalf("expected lfs oid sha256:deadbeef, got %q", pathCache.LFSOID)
	}

	oidEntry := oidEntryFile(oidsDir, "sha256:deadbeef")
	oidData, err := os.ReadFile(oidEntry)
	if err != nil {
		t.Fatalf("read oid entry: %v", err)
	}
	var oidCache OIDEntry
	if err := json.Unmarshal(oidData, &oidCache); err != nil {
		t.Fatalf("unmarshal oid entry: %v", err)
	}
	if oidCache.LFSOID != "sha256:deadbeef" {
		t.Fatalf("expected oid entry sha256:deadbeef, got %q", oidCache.LFSOID)
	}
	if len(oidCache.Paths) != 1 || oidCache.Paths[0] != "data/file.bin" {
		t.Fatalf("expected oid paths to include data/file.bin, got %v", oidCache.Paths)
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test User")
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
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
}
