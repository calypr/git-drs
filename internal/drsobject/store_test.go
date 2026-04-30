package drsobject

import (
	"path/filepath"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestWriteReadObject(t *testing.T) {
	tmp := t.TempDir()
	basePath := filepath.Join(tmp, ".git", "drs", "objects")
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	obj := &drsapi.DrsObject{
		Id:        "did-1",
		Name:      ptrString("file.txt"),
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
	}
	if err := WriteObject(basePath, obj, oid); err != nil {
		t.Fatalf("WriteObject error: %v", err)
	}
	read, err := ReadObject(basePath, oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	if read.Id != "did-1" {
		t.Fatalf("unexpected object: %+v", read)
	}
}

func TestWriteObjectBasePath(t *testing.T) {
	path, err := objectPath(".git/drs/objects", "short")
	if err == nil {
		t.Fatalf("expected validation error, got %s", path)
	}
}

func ptrString(s string) *string { return &s }
