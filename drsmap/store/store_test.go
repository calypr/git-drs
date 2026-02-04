package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/git-drs/drs/hash"
)

func TestWriteAndReadObject(t *testing.T) {
	baseDir := filepath.Join(t.TempDir(), "drs")
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	obj := &drs.DRSObject{
		Id:        "did-1",
		Name:      "file.txt",
		Checksums: hash.HashInfo{SHA256: oid},
	}

	if err := WriteObject(baseDir, obj, oid); err != nil {
		t.Fatalf("WriteObject error: %v", err)
	}

	read, err := ReadObject(baseDir, oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	if read.Id != obj.Id {
		t.Fatalf("unexpected object: %+v", read)
	}

	path, err := ObjectPath(baseDir, oid)
	if err != nil {
		t.Fatalf("ObjectPath error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected object at %s: %v", path, err)
	}
}
