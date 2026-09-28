package lsfiles

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
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
	if got, want := browseEntryNames(listings[0].entries), []string{"subdir", "visible.txt"}; !equalStrings(got, want) {
		t.Fatalf("visible entries = %v, want %v", got, want)
	}
	if !listings[0].entries[0].mode.IsDir() {
		t.Fatalf("first entry mode = %v, want a directory", listings[0].entries[0].mode)
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
	if got, want := browseEntryNames(listings[0].entries), []string{"inside.txt"}; !equalStrings(got, want) {
		t.Fatalf("directory operand entries = %v, want %v", got, want)
	}
	if got, want := browseEntryNames(listings[1].entries), []string{file}; !equalStrings(got, want) {
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
	if got, want := browseEntryNames(listings[0].entries), []string{path}; !equalStrings(got, want) {
		t.Fatalf("broken symlink operand = %v, want %v", got, want)
	}
}

func TestPrintBrowseListingsLongHumanReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	writeBrowseFile(t, path, strings.Repeat("x", 1536))
	listings, err := collectBrowseListings([]string{path})
	if err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&output)
	if err := printBrowseListings(cmd, listings, true, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), listings[0].entries[0].mode.String()) ||
		!strings.Contains(output.String(), "1.5K") || !strings.Contains(output.String(), "large.bin") {
		t.Fatalf("long human-readable output = %q", output.String())
	}
	if got := formatBrowseSize(1536, true); got != "1.5K" {
		t.Fatalf("human size = %q, want 1.5K", got)
	}
	if got := formatBrowseSize(1536, false); got != "1536" {
		t.Fatalf("byte size = %q, want 1536", got)
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

func browseEntryNames(entries []browseEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.name)
	}
	return names
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
