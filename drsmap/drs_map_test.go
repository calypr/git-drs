package drsmap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
)

func setupTestRepo(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
}

func TestWriteAndReadDrsObject(t *testing.T) {
	setupTestRepo(t)
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path, err := GetObjectPath(".git/drs/lfs/objects", oid)
	if err != nil {
		t.Fatalf("GetObjectPath error: %v", err)
	}

	obj := &drs.DRSObject{
		Id:        "did-1",
		Name:      "file.txt",
		Checksums: hash.HashInfo{SHA256: oid},
	}

	if err := WriteDrsObj(obj, oid, path); err != nil {
		t.Fatalf("WriteDrsObj error: %v", err)
	}

	read, err := DrsInfoFromOid(oid)
	if err != nil {
		t.Fatalf("DrsInfoFromOid error: %v", err)
	}
	if read.Id != "did-1" {
		t.Fatalf("unexpected object: %+v", read)
	}
}

func TestGetObjectPathValidation(t *testing.T) {
	if _, err := GetObjectPath(".git/drs/lfs/objects", "short"); err == nil {
		t.Fatalf("expected error for invalid oid")
	}
}

func TestDrsUUIDDeterministic(t *testing.T) {
	id1 := DrsUUID("project", "hash")
	id2 := DrsUUID("project", "hash")
	if id1 != id2 {
		t.Fatalf("expected deterministic UUIDs, got %s vs %s", id1, id2)
	}
}

func TestGetObjectPathLayout(t *testing.T) {
	oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	path, err := GetObjectPath("base", oid)
	if err != nil {
		t.Fatalf("GetObjectPath error: %v", err)
	}
	if filepath.Base(path) != oid {
		t.Fatalf("unexpected path: %s", path)
	}
}
