package addref

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/syfon/client/drs"
)

func TestCreateLfsPointer(t *testing.T) {
	obj := &drs.DRSObject{
		Size:      10,
		Checksums: []drs.Checksum{{Type: "sha256", Checksum: "abc"}},
	}
	path := filepath.Join(t.TempDir(), "pointer")
	if err := lfs.CreateLfsPointer(obj, path); err != nil {
		t.Fatalf("CreateLfsPointer error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected pointer content")
	}
}

func TestCreateLfsPointer_NoChecksum(t *testing.T) {
	obj := &drs.DRSObject{}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing checksums")
	}
}

func TestCreateLfsPointer_NoSHA256(t *testing.T) {
	obj := &drs.DRSObject{Checksums: []drs.Checksum{{Type: "md5", Checksum: "md5"}}}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing sha256")
	}
}
