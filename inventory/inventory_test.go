package inventory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestListReadsCommittedPointersAfterWorktreeHydration(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path := filepath.Join(repo, "data", "sample.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + sha + "\nsize 42\n"
	if err := os.WriteFile(path, []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "META"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "META", "DocumentReference.ndjson"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "CONFIG"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CONFIG", "project.json"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add pointers")
	if err := os.WriteFile(path, []byte("hydrated bytes that are not an LFS pointer"), 0o644); err != nil {
		t.Fatal(err)
	}

	pointers, err := List(context.Background(), Options{RepositoryRoot: repo, Ref: "HEAD", ExcludePrefixes: []string{"META", "CONFIG"}})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(pointers) != 1 || pointers[0].Path != "data/sample.bin" || pointers[0].SHA256 != sha || pointers[0].Size != 42 {
		t.Fatalf("unexpected pointers: %#v", pointers)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
