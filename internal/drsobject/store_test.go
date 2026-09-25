package drsobject

import (
	"path/filepath"
	"strings"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/drs"
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

func TestWriteObjectRejectsNonHexObjectIDs(t *testing.T) {
	for _, oid := range []string{strings.Repeat("g", 64), strings.Repeat("/", 64), strings.Repeat("a", 63) + "/"} {
		if path, err := objectPath(filepath.Join(t.TempDir(), "objects"), oid); err == nil {
			t.Fatalf("objectPath accepted %q as %s", oid, path)
		}
	}
}

func TestObjectPathCanonicalizesUppercaseSHA256(t *testing.T) {
	path, err := objectPath(filepath.Join(t.TempDir(), "objects"), "SHA256:"+strings.Repeat("A", 64))
	if err != nil {
		t.Fatalf("objectPath returned error: %v", err)
	}
	if !strings.HasSuffix(path, filepath.Join("aa", "aa", strings.Repeat("a", 64))) {
		t.Fatalf("objectPath = %q, want canonical lowercase fanout", path)
	}
}

func ptrString(s string) *string { return &s }
