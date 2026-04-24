package lfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
)

func TestAddFilesFromDryRunIncludesPointerEntry(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "data", "pointer.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	oid := "1111111111111111111111111111111111111111111111111111111111111111"
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 123\n"
	if err := os.WriteFile(path, []byte(pointer), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}

	out := "push " + oid + " data/pointer.bin"
	got := map[string]LfsFileInfo{}
	logger := drslog.NewNoOpLogger()
	if err := addFilesFromDryRun(out, repo, logger, got); err != nil {
		t.Fatalf("addFilesFromDryRun error: %v", err)
	}

	info, ok := got["data/pointer.bin"]
	if !ok {
		t.Fatalf("expected pointer file to be included in dry-run set")
	}
	if !info.IsPointer {
		t.Fatalf("expected IsPointer=true for pointer entry")
	}
	if info.Oid != oid {
		t.Fatalf("expected oid %s, got %s", oid, info.Oid)
	}
}
