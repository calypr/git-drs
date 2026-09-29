package precommit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/precommit_cache"
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

	cache := &precommit_cache.Cache{Root: cacheRoot, PathsDir: pathsDir, OIDsDir: oidsDir}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := handleUpsert(context.Background(), cache, "data/file.txt", now); err != nil {
		t.Fatalf("handleUpsert: %v", err)
	}

	pathEntry := precommit_cache.PathEntryPath(cache, "data/file.txt")
	if _, err := os.Stat(pathEntry); !os.IsNotExist(err) {
		t.Fatalf("expected no cache entry for non-LFS file, got err=%v", err)
	}
}

func TestStagedBlobLookupUsesLiteralColonPrefixedPath(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	const largeName = "0:big.bin"
	const largeSize = 2 << 20
	if err := os.WriteFile(filepath.Join(repo, largeName), bytes.Repeat([]byte{'x'}, largeSize), 0o644); err != nil {
		t.Fatal(err)
	}
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + strings.Repeat("a", 64) + "\nsize 7\n"
	if err := os.WriteFile(filepath.Join(repo, "big.bin"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "--", largeName, "big.bin")

	size, err := stagedBlobSize(context.Background(), largeName)
	if err != nil || size != largeSize {
		t.Fatalf("staged size for %q = %d, %v; want %d", largeName, size, err, largeSize)
	}
	if oid, isPointer, err := stagedLFSOID(context.Background(), largeName); err != nil || isPointer {
		t.Fatalf("staged file selected sibling pointer: oid=%q isPointer=%v err=%v", oid, isPointer, err)
	}
}

func TestTypeChangeFromSymlinkWarnsForStagedRegularFile(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	path := filepath.Join(repo, "data.bin")
	if err := os.Symlink("old-target", path); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "data.bin")
	gitCmd(t, repo, "commit", "-m", "add symlink")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("oversized plain data"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, repo, "add", "data.bin")

	changes, err := stagedChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	files, err := collectOversizedPlainGitStagedFiles(context.Background(), changes, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "data.bin" || files[0].Size != int64(len("oversized plain data")) {
		t.Fatalf("type change omitted from warning: changes=%+v files=%+v", changes, files)
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

	cache := &precommit_cache.Cache{Root: cacheRoot, PathsDir: pathsDir, OIDsDir: oidsDir}
	now := time.Now().UTC().Format(time.RFC3339)
	if err := handleUpsert(context.Background(), cache, "data/file.bin", now); err != nil {
		t.Fatalf("handleUpsert: %v", err)
	}

	pathEntry := precommit_cache.PathEntryPath(cache, "data/file.bin")
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

	oidEntry := precommit_cache.OIDEntryPath(cache, "sha256:deadbeef")
	oidData, err := os.ReadFile(oidEntry)
	if err != nil {
		t.Fatalf("read oid entry: %v", err)
	}
	var oidCache precommit_cache.OIDEntry
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

func TestRunWritesTombstoneForDeletedLFSPointer(t *testing.T) {
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
	gitCmd(t, repo, "commit", "-m", "add pointer")

	cache, err := precommit_cache.Open(context.Background())
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	if err := precommit_cache.EnsureLayout(cache); err != nil {
		t.Fatalf("ensure cache layout: %v", err)
	}
	if err := precommit_cache.WritePathEntry(cache, precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    "sha256:deadbeef",
		UpdatedAt: "2026-01-01T00:00:00Z",
	}); err != nil {
		t.Fatalf("write path entry: %v", err)
	}
	if err := precommit_cache.UpsertOIDPath(cache, "sha256:deadbeef", "", "data/file.bin", "", "2026-01-01T00:00:00Z", false); err != nil {
		t.Fatalf("write oid entry: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove working tree file: %v", err)
	}
	gitCmd(t, repo, "add", "-u")
	if err := run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}

	if _, err := os.Stat(precommit_cache.PathEntryPath(cache, "data/file.bin")); !os.IsNotExist(err) {
		t.Fatalf("expected deleted path entry to be removed, got err=%v", err)
	}
	tombstone := filepath.Join(cache.Root, "tombstones", precommit_cache.EncodePath("data/file.bin")+".json")
	data, err := os.ReadFile(tombstone)
	if err != nil {
		t.Fatalf("read tombstone: %v", err)
	}
	var tombstoneEntry map[string]string
	if err := json.Unmarshal(data, &tombstoneEntry); err != nil {
		t.Fatalf("unmarshal tombstone: %v", err)
	}
	if tombstoneEntry["path"] != "data/file.bin" {
		t.Fatalf("expected tombstone path data/file.bin, got %q", tombstoneEntry["path"])
	}
	if tombstoneEntry["deleted_at"] == "" {
		t.Fatal("expected tombstone deleted_at")
	}
	if info, err := os.Stat(tombstone); err != nil {
		t.Fatalf("stat tombstone: %v", err)
	} else if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("expected tombstone permissions 0644, got %o", got)
	}
	if _, err := os.Stat(tombstone + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("expected tombstone temporary file to be removed, got err=%v", err)
	}
}

func TestCollectOversizedPlainGitStagedFiles(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	plainPath := filepath.Join(repo, "data", "large.bin")
	if err := os.MkdirAll(filepath.Dir(plainPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(plainPath, []byte("plain oversized payload"), 0o644); err != nil {
		t.Fatalf("write plain file: %v", err)
	}
	gitCmd(t, repo, "add", "data/large.bin")

	pointerPath := filepath.Join(repo, "data", "pointer.bin")
	lfsPointer := strings.Join([]string{
		"version https://git-lfs.github.com/spec/v1",
		"oid sha256:deadbeef",
		"size 999",
		"",
	}, "\n")
	if err := os.WriteFile(pointerPath, []byte(lfsPointer), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
	gitCmd(t, repo, "add", "data/pointer.bin")

	changes, err := stagedChanges(context.Background())
	if err != nil {
		t.Fatalf("stagedChanges: %v", err)
	}
	files, err := collectOversizedPlainGitStagedFiles(context.Background(), changes, 1)
	if err != nil {
		t.Fatalf("collectOversizedPlainGitStagedFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 oversized plain file, got %d: %+v", len(files), files)
	}
	if files[0].Path != "data/large.bin" {
		t.Fatalf("unexpected oversized file path: %+v", files[0])
	}
}

func TestRunAbortsWhenOversizedPlainGitCommitIsRejected(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := filepath.Join(repo, "data", "large.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("plain oversized payload"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, repo, "add", "data/large.bin")

	oldThreshold := directCommitWarningThresholdBytes
	oldPrompt := confirmOversizedDirectGitCommit
	t.Cleanup(func() {
		directCommitWarningThresholdBytes = oldThreshold
		confirmOversizedDirectGitCommit = oldPrompt
	})
	directCommitWarningThresholdBytes = 1
	confirmOversizedDirectGitCommit = func(files []OversizedStagedFile) (bool, error) {
		if len(files) != 1 || files[0].Path != "data/large.bin" {
			t.Fatalf("unexpected prompt files: %+v", files)
		}
		return false, nil
	}

	err := run(context.Background())
	if err == nil {
		t.Fatal("expected run to abort when oversized file warning is rejected")
	}
	if !strings.Contains(err.Error(), "commit aborted") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunWarnsForOversizedFileWithNewlineInPath(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := "large\nfile.bin"
	if err := os.WriteFile(filepath.Join(repo, path), []byte("plain oversized payload"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, repo, "add", "--", path)

	oldThreshold := directCommitWarningThresholdBytes
	oldPrompt := confirmOversizedDirectGitCommit
	t.Cleanup(func() {
		directCommitWarningThresholdBytes = oldThreshold
		confirmOversizedDirectGitCommit = oldPrompt
	})
	directCommitWarningThresholdBytes = 1
	called := false
	confirmOversizedDirectGitCommit = func(files []OversizedStagedFile) (bool, error) {
		called = true
		if len(files) != 1 || files[0].Path != path {
			t.Fatalf("unexpected warning files: %+v", files)
		}
		return false, nil
	}

	err := run(context.Background())
	if !called || err == nil || !strings.Contains(err.Error(), "commit aborted") {
		t.Fatalf("expected warning to abort commit for %q, called=%v err=%v", path, called, err)
	}
}

func TestRunWarnsForActualOversizedStagedBlob(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := "large\nfile.bin"
	file, err := os.Create(filepath.Join(repo, path))
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := file.Truncate(defaultDirectCommitWarningThreshold + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close file: %v", err)
	}
	gitCmd(t, repo, "add", "--", path)

	oldPrompt := confirmOversizedDirectGitCommit
	t.Cleanup(func() { confirmOversizedDirectGitCommit = oldPrompt })
	called := false
	confirmOversizedDirectGitCommit = func(files []OversizedStagedFile) (bool, error) {
		called = true
		if len(files) != 1 || files[0].Path != path || files[0].Size <= defaultDirectCommitWarningThreshold {
			t.Fatalf("unexpected warning files: %+v", files)
		}
		return false, nil
	}

	err = run(context.Background())
	if !called || err == nil || !strings.Contains(err.Error(), "commit aborted") {
		t.Fatalf("expected warning to abort oversized commit, called=%v err=%v", called, err)
	}
}

func TestRunAcceptsPlainBlobAtPointerReadLimit(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := "single-line.bin"
	if err := os.WriteFile(filepath.Join(repo, path), bytes.Repeat([]byte{'x'}, int(lfs.MaxPointerFileBytes)), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, repo, "add", "--", path)
	if err := run(context.Background()); err != nil {
		t.Fatalf("plain staged blob should pass: %v", err)
	}
}

func TestRunCachesPointerWithNewlineInPath(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	path := "pointer\nfile.bin"
	oid := strings.Repeat("a", 64)
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 4\n"
	if err := os.WriteFile(filepath.Join(repo, path), []byte(pointer), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	gitCmd(t, repo, "add", "--", path)
	if err := run(context.Background()); err != nil {
		t.Fatalf("run: %v", err)
	}
	cache, err := precommit_cache.Open(context.Background())
	if err != nil {
		t.Fatalf("open cache: %v", err)
	}
	entry, ok, err := precommit_cache.ReadPathEntry(cache, path)
	if err != nil || !ok || entry.LFSOID != "sha256:"+oid {
		t.Fatalf("missing pointer cache entry for %q: entry=%+v ok=%v err=%v", path, entry, ok, err)
	}
}

func TestStagedChangesPreservesRenamedPathWithNewline(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	oldPath := "old.bin"
	newPath := "new\nname.bin"
	if err := os.WriteFile(filepath.Join(repo, oldPath), []byte("pointer content"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitCmd(t, repo, "add", "--", oldPath)
	gitCmd(t, repo, "commit", "-m", "add file")
	if err := os.Rename(filepath.Join(repo, oldPath), filepath.Join(repo, newPath)); err != nil {
		t.Fatalf("rename file: %v", err)
	}
	gitCmd(t, repo, "add", "-A")

	changes, err := stagedChanges(context.Background())
	if err != nil {
		t.Fatalf("stagedChanges: %v", err)
	}
	if len(changes) != 1 || changes[0] != (Change{Kind: KindRename, OldPath: oldPath, NewPath: newPath}) {
		t.Fatalf("unexpected staged changes: %+v", changes)
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
