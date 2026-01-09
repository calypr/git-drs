package drsmap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
)

func TestCreateLfsPointer(t *testing.T) {
	obj := &drs.DRSObject{
		Size:      10,
		Checksums: hash.HashInfo{SHA256: "abc"},
	}
	path := filepath.Join(t.TempDir(), "pointer")
	if err := CreateLfsPointer(obj, path); err != nil {
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
	if err := CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing checksums")
	}
}

func TestCreateLfsPointer_NoSHA256(t *testing.T) {
	obj := &drs.DRSObject{Checksums: hash.HashInfo{MD5: "md5"}}
	if err := CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing sha256")
	}
}
