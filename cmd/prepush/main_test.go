package prepush

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/precommit_cache"
	"github.com/maypok86/otter"
)

func TestLfsFilesFromCache(t *testing.T) {
	repo := setupGitRepo(t)
	filePath := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "first")
	oldSHA := gitOutputString(t, repo, "rev-parse", "HEAD")

	if err := os.WriteFile(filePath, []byte("second"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "second")
	newSHA := gitOutputString(t, repo, "rev-parse", "HEAD")

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")

	cache, err := makeCacheForTesting(t, repo, cacheRoot)
	if err != nil {
		t.Fatalf("makeCacheForTesting: %v", err)
	}

	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths dir: %v", err)
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		t.Fatalf("mkdir oids dir: %v", err)
	}

	pathEntry := precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    "oid-123",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	pathEntryFile := filepath.Join(cache.PathsDir, precommit_cache.EncodePath(pathEntry.Path)+".json")
	writeJSON(t, pathEntryFile, pathEntry)

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refs := []pushedRef{{
		LocalRef:  "refs/heads/main",
		LocalSHA:  newSHA,
		RemoteRef: "refs/heads/main",
		RemoteSHA: oldSHA,
	}}

	lfsFiles, ok, err := lfsFilesFromCache(context.Background(), &cache, refs, logger)
	if err != nil {
		t.Fatalf("lfsFilesFromCache: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache to be usable")
	}
	info, exists := lfsFiles["data/file.bin"]
	if !exists {
		t.Fatalf("expected lfs info for data/file.bin")
	}
	if info.Oid != "oid-123" {
		t.Fatalf("expected oid to be oid-123, got %s", info.Oid)
	}
	if info.OidType != "sha256" {
		t.Fatalf("expected oid type sha256, got %s", info.OidType)
	}
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != stat.Size() {
		t.Fatalf("expected size %d, got %d", stat.Size(), info.Size)
	}
}

func makeCacheForTesting(t *testing.T, repo string, cacheRoot string) (precommit_cache.Cache, error) {
	pc, err := otter.MustBuilder[string, *precommit_cache.PathEntry](10000).Build()
	if err != nil {
		t.Fatalf("create path cache: %v", err)
	}
	oc, err := otter.MustBuilder[string, *precommit_cache.OIDEntry](1000).Build() // OIDs are fewer than paths usually
	if err != nil {
		t.Fatalf("create oid cache: %v", err)
	}

	cache := &precommit_cache.Cache{
		GitDir:    filepath.Join(repo, ".git"),
		Root:      cacheRoot,
		PathsDir:  filepath.Join(cacheRoot, "paths"),
		OIDsDir:   filepath.Join(cacheRoot, "oids"),
		StatePath: filepath.Join(cacheRoot, "state.json"),
		PathCache: pc,
		OidCache:  oc,
	}
	return *cache, err
}

func TestLfsFilesFromCacheStale(t *testing.T) {
	repo := setupGitRepo(t)
	filePath := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "first")
	sha := gitOutputString(t, repo, "rev-parse", "HEAD")

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	cache, err := makeCacheForTesting(t, repo, cacheRoot)
	if err != nil {
		t.Fatalf("makeCacheForTesting: %v", err)
	}

	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths dir: %v", err)
	}

	pathEntry := precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    "oid-123",
		UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	}
	pathEntryFile := filepath.Join(cache.PathsDir, precommit_cache.EncodePath(pathEntry.Path)+".json")
	writeJSON(t, pathEntryFile, pathEntry)

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refs := []pushedRef{{
		LocalRef:  "refs/heads/main",
		LocalSHA:  sha,
		RemoteRef: "refs/heads/main",
		RemoteSHA: "0000000000000000000000000000000000000000",
	}}

	_, ok, err := lfsFilesFromCache(context.Background(), &cache, refs, logger)
	if err != nil {
		t.Fatalf("lfsFilesFromCache: %v", err)
	}
	if ok {
		t.Fatalf("expected cache to be stale")
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

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
}

func gitOutputString(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
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
