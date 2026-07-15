package lfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPointerInventoryForObjectsFindsHistoricalDeletedPointer(t *testing.T) {
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	gitTestCommand(t, repo, "config", "user.email", "test@example.com")
	gitTestCommand(t, repo, "config", "user.name", "test")
	path := filepath.Join(repo, "data.bin")
	if err := os.WriteFile(path, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nsize 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repo, "add", "data.bin")
	gitTestCommand(t, repo, "commit", "-qm", "add pointer")
	first := gitTestCommand(t, repo, "rev-parse", "HEAD")
	gitTestCommand(t, repo, "rm", "-q", "data.bin")
	gitTestCommand(t, repo, "commit", "-qm", "delete pointer")
	second := gitTestCommand(t, repo, "rev-parse", "HEAD")

	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })

	files, err := PointerInventoryForObjects(context.Background(), []string{second}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected historical pointer, got %+v", files)
	}
	if files["data.bin"].Oid != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("unexpected pointer: %+v", files)
	}

	files, err = PointerInventoryForObjects(context.Background(), []string{second}, []string{first})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("expected acknowledged history to be excluded, got %+v", files)
	}
}

func gitTestCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
