package lfs

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/common"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestWriteReadObject(t *testing.T) {
	tmp := t.TempDir()
	store := NewObjectStore(filepath.Join(tmp, ".git", "drs", "objects"), slog.New(slog.NewTextHandler(os.Stderr, nil)))
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	obj := &drsapi.DrsObject{
		Id:        "did-1",
		Name:      ptrString("file.txt"),
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
	}
	if err := store.WriteObject(obj, oid); err != nil {
		t.Fatalf("WriteObject error: %v", err)
	}
	read, err := store.ReadObject(oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	if read.Id != "did-1" {
		t.Fatalf("unexpected object: %+v", read)
	}
}

func TestWriteObjectBasePath(t *testing.T) {
	path, err := ObjectPath(".git/drs/objects", "short")
	if err == nil {
		t.Fatalf("expected validation error, got %s", path)
	}
}

func TestGetDrsLfsObjectsEmpty(t *testing.T) {
	_ = common.DRS_OBJS_PATH
}
