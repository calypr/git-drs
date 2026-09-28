package gitrepo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeWorktreePathPreservesSpacesAndRejectsSymlinks(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, " data ", " file.bin ")
	got, err := SafeWorktreePath(root, " data / file.bin ")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("SafeWorktreePath = %q, want %q", got, want)
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := SafeWorktreePath(root, "link/file.bin"); err == nil {
		t.Fatal("SafeWorktreePath accepted a symlinked parent")
	}
}
