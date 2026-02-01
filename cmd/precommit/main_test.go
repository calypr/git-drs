package precommit

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	lfsOIDOne = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	lfsOIDTwo = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
)

func TestStagedLFSOID(t *testing.T) {
	repo := setupGitRepo(t)
	writeFile(t, filepath.Join(repo, "data.bin"), lfsPointer(lfsOIDOne))
	gitCmd(t, repo, "add", "data.bin")

	oid, ok, err := stagedLFSOID(context.Background(), "data.bin")
	if err != nil {
		t.Fatalf("stagedLFSOID error: %v", err)
	}
	if !ok {
		t.Fatalf("expected LFS pointer to be detected")
	}
	if oid != lfsOIDOne {
		t.Fatalf("expected oid %q, got %q", lfsOIDOne, oid)
	}
}

func TestStagedLFSOIDNonLFS(t *testing.T) {
	repo := setupGitRepo(t)
	writeFile(t, filepath.Join(repo, "notes.txt"), "plain text\n")
	gitCmd(t, repo, "add", "notes.txt")

	_, ok, err := stagedLFSOID(context.Background(), "notes.txt")
	if err != nil {
		t.Fatalf("stagedLFSOID error: %v", err)
	}
	if ok {
		t.Fatalf("expected non-LFS file to be out of scope")
	}
}

func TestHandleUpsertAndDelete(t *testing.T) {
	repo := setupGitRepo(t)
	writeFile(t, filepath.Join(repo, "data", "foo.bin"), lfsPointer(lfsOIDOne))
	gitCmd(t, repo, "add", "data/foo.bin")

	gitDir := gitDirPath(t, repo)
	cacheRoot := filepath.Join(gitDir, cacheVersionDir)
	pathsDir := filepath.Join(cacheRoot, "paths")
	oidsDir := filepath.Join(cacheRoot, "oids")
	tombsDir := filepath.Join(cacheRoot, "tombstones")
	now := "2026-02-01T12:34:56Z"

	if err := handleUpsert(context.Background(), pathsDir, oidsDir, "data/foo.bin", now); err != nil {
		t.Fatalf("handleUpsert error: %v", err)
	}

	pathEntry := readPathEntry(t, pathEntryFile(pathsDir, "data/foo.bin"))
	if pathEntry.Path != "data/foo.bin" || pathEntry.LFSOID != lfsOIDOne {
		t.Fatalf("unexpected path entry: %#v", pathEntry)
	}

	oidEntry := readOIDEntry(t, oidEntryFile(oidsDir, lfsOIDOne))
	if !contains(oidEntry.Paths, "data/foo.bin") {
		t.Fatalf("expected path in oid entry, got %#v", oidEntry.Paths)
	}
	if oidEntry.ContentChange {
		t.Fatalf("expected content_changed to be false")
	}

	writeFile(t, filepath.Join(repo, "data", "foo.bin"), lfsPointer(lfsOIDTwo))
	gitCmd(t, repo, "add", "data/foo.bin")

	if err := handleUpsert(context.Background(), pathsDir, oidsDir, "data/foo.bin", now); err != nil {
		t.Fatalf("handleUpsert (content change) error: %v", err)
	}

	updated := readOIDEntry(t, oidEntryFile(oidsDir, lfsOIDTwo))
	if !updated.ContentChange {
		t.Fatalf("expected content_changed to be true after content update")
	}
	if !contains(updated.Paths, "data/foo.bin") {
		t.Fatalf("expected new oid entry to include path, got %#v", updated.Paths)
	}

	if err := handleDelete(context.Background(), pathsDir, oidsDir, tombsDir, "data/foo.bin", now); err != nil {
		t.Fatalf("handleDelete error: %v", err)
	}

	if _, err := os.Stat(pathEntryFile(pathsDir, "data/foo.bin")); !os.IsNotExist(err) {
		t.Fatalf("expected path entry to be removed")
	}

	deletedEntry := readOIDEntry(t, oidEntryFile(oidsDir, lfsOIDTwo))
	if contains(deletedEntry.Paths, "data/foo.bin") {
		t.Fatalf("expected path to be removed from oid entry, got %#v", deletedEntry.Paths)
	}
	if _, err := os.Stat(filepath.Join(tombsDir, encodePath("data/foo.bin")+".json")); err != nil {
		t.Fatalf("expected tombstone to be written: %v", err)
	}
}

func TestStagedChangesRename(t *testing.T) {
	repo := setupGitRepo(t)
	writeFile(t, filepath.Join(repo, "a.txt"), "alpha\n")
	gitCmd(t, repo, "add", "a.txt")
	gitCmd(t, repo, "commit", "-m", "init")

	gitCmd(t, repo, "mv", "a.txt", "b.txt")
	changes, err := stagedChanges(context.Background())
	if err != nil {
		t.Fatalf("stagedChanges error: %v", err)
	}

	found := false
	for _, ch := range changes {
		if ch.Kind == KindRename && ch.OldPath == "a.txt" && ch.NewPath == "b.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected rename in staged changes, got %#v", changes)
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test User")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := execCmd(dir, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func execCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func lfsPointer(oid string) string {
	return strings.Join([]string{
		lfsSpecLine,
		"oid " + oid,
		"size 123",
		"",
	}, "\n")
}

func gitDirPath(t *testing.T, repo string) string {
	t.Helper()
	cmd := execCmd(repo, "rev-parse", "--git-dir")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse --git-dir failed: %v (%s)", err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func readPathEntry(t *testing.T, path string) PathEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path entry: %v", err)
	}
	var entry PathEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal path entry: %v", err)
	}
	return entry
}

func readOIDEntry(t *testing.T, path string) OIDEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read oid entry: %v", err)
	}
	var entry OIDEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatalf("unmarshal oid entry: %v", err)
	}
	return entry
}

func contains(paths []string, value string) bool {
	for _, p := range paths {
		if p == value {
			return true
		}
	}
	return false
}
