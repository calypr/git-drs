package lfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIsLFSTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	repo := t.TempDir()
	mustRun(t, repo, "git", "init")

	// Add an LFS tracking rule (we only need attributes; git-lfs binary not required)
	attr := []byte("*.dat filter=lfs diff=lfs merge=lfs -text\n")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), attr, 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	// Create files
	tracked := filepath.Join(repo, "data", "file.dat")
	untracked := filepath.Join(repo, "data", "file.txt")
	if err := os.MkdirAll(filepath.Dir(tracked), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tracked, []byte("x"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(untracked, []byte("y"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	// Run from inside repo so git check-attr works
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	// Verify tracked
	got, err := IsLFSTracked("data/file.dat")
	if err != nil {
		t.Fatalf("IsLFSTracked tracked: %v", err)
	}
	if !got {
		t.Fatalf("expected data/file.dat to be LFS tracked")
	}

	// Verify untracked
	got, err = IsLFSTracked("data/file.txt")
	if err != nil {
		t.Fatalf("IsLFSTracked untracked: %v", err)
	}
	if got {
		t.Fatalf("expected data/file.txt to NOT be LFS tracked")
	}
}

func TestIsLFSTrackedFiltersNoise(t *testing.T) {
	fakeGitDir := t.TempDir()
	fakeGitPath := filepath.Join(fakeGitDir, "git")

	script := `#!/bin/sh
path="$4"
echo 'debug: verbose output'
echo 'other/file: filter: lfs'
if [ "$path" = "match/file" ]; then
  printf '%s: filter: lfs\n' "$path"
fi
`

	if err := os.WriteFile(fakeGitPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	t.Setenv("PATH", fakeGitDir)

	got, err := IsLFSTracked("no/match")
	if err != nil {
		t.Fatalf("IsLFSTracked no match: %v", err)
	}
	if got {
		t.Fatalf("expected no/match to be NOT tracked")
	}

	got, err = IsLFSTracked("match/file")
	if err != nil {
		t.Fatalf("IsLFSTracked match: %v", err)
	}
	if !got {
		t.Fatalf("expected match/file to be LFS tracked")
	}
}
