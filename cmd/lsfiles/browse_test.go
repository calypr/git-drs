package lsfiles

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCollectBrowseListingsDefaultShowsVisibleFilesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	writeBrowseFile(t, filepath.Join(dir, "visible.txt"), "content")
	writeBrowseFile(t, filepath.Join(dir, ".hidden"), "hidden")
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeBrowseFile(t, filepath.Join(dir, "subdir", "inside.txt"), "content")

	withWorkingDirectory(t, dir)
	listings, err := collectBrowseListings(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 {
		t.Fatalf("got %d listings, want 1", len(listings))
	}
	if got, want := listings[0].entries, []string{"subdir", "visible.txt"}; !slices.Equal(got, want) {
		t.Fatalf("visible entries = %v, want %v", got, want)
	}
}

func TestCollectBrowseListingsPathOperands(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeBrowseFile(t, filepath.Join(subdir, "inside.txt"), "content")
	file := filepath.Join(dir, "file.txt")
	writeBrowseFile(t, file, "content")

	listings, err := collectBrowseListings([]string{subdir, file})
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 2 {
		t.Fatalf("got %d listings, want 2", len(listings))
	}
	if got, want := listings[0].entries, []string{"inside.txt"}; !slices.Equal(got, want) {
		t.Fatalf("directory operand entries = %v, want %v", got, want)
	}
	if got, want := listings[1].entries, []string{file}; !slices.Equal(got, want) {
		t.Fatalf("file operand entry = %v, want %v", got, want)
	}
}

func TestCollectBrowseListingsExplicitBrokenSymlink(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken-link")
	if err := os.Symlink("missing-target", path); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	listings, err := collectBrowseListings([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := listings[0].entries, []string{path}; !slices.Equal(got, want) {
		t.Fatalf("broken symlink operand = %v, want %v", got, want)
	}
}

func withWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func writeBrowseFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
