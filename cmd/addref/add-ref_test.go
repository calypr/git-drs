package addref

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestCreateLfsPointer(t *testing.T) {
	obj := &drsapi.DrsObject{
		Size:      10,
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
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
	obj := &drsapi.DrsObject{}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing checksums")
	}
}

func TestCreateLfsPointer_NoSHA256(t *testing.T) {
	obj := &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "md5", Checksum: "md5"}}}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing sha256")
	}
}

func TestAddRefLocalOIDUsesDerivedSourceWhenSHA256Missing(t *testing.T) {
	obj := &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "md5", Checksum: "md5"}}}
	oid := addRefLocalOID("drs://example.org/object-1", "source", obj)
	if len(oid) != 64 {
		t.Fatalf("expected sha256-shaped derived oid, got %q", oid)
	}
	if oid != derivedSourceOID("drs://example.org/object-1", "source") {
		t.Fatalf("expected derived source oid, got %s", oid)
	}
}

func TestCreateDRSPointerPreservesSourceURI(t *testing.T) {
	obj := &drsapi.DrsObject{Size: 42}
	path := filepath.Join(t.TempDir(), "pointer")
	if err := lfs.CreateDRSPointer(obj, path, "drs://example.org/object-1"); err != nil {
		t.Fatalf("CreateDRSPointer error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	expected := "version https://calypr.github.io/spec/v1\noid drs://example.org/object-1\nsize 42\n"
	if string(data) != expected {
		t.Fatalf("pointer mismatch: expected %q, got %q", expected, string(data))
	}
}
