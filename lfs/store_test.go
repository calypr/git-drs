package lfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/drslog"
)

func setupTempRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	cmd := exec.Command("git", "init", repo)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	return repo
}

func TestGetPendingObjects(t *testing.T) {
	setupTempRepo(t)
	objectsDir := filepath.Join(".git", "drs", "lfs", "objects", "aa", "bb")
	if err := os.MkdirAll(objectsDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(objectsDir, "oid-1"), []byte(""), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".git", "drs", "lfs", "objects", "aa", "oid-2"), []byte(""), 0o644); err != nil {
		t.Fatalf("write malformed file failed: %v", err)
	}

	logger := drslog.NewNoOpLogger()
	objects, err := GetPendingObjects(logger)
	if err != nil {
		t.Fatalf("GetPendingObjects error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 pending object, got %d", len(objects))
	}
	if objects[0].OID != "oid-1" {
		t.Fatalf("expected OID oid-1, got %s", objects[0].OID)
	}
}

func TestGetDrsLfsObjects(t *testing.T) {
	setupTempRepo(t)
	objectsDir := filepath.Join(".git", "drs", "lfs", "objects", "aa", "bb")
	if err := os.MkdirAll(objectsDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	payload := drs.DRSObject{
		Id:        "object-1",
		Name:      "object-one",
		Checksums: hash.HashInfo{SHA256: "sha-256-value"},
	}
	data, err := sonic.ConfigFastest.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(objectsDir, "oid-1"), data, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(objectsDir, "bad-json"), []byte("{invalid"), 0o644); err != nil {
		t.Fatalf("write invalid json failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(".git", "drs", "lfs", "objects", "aa", "oid-2"), data, 0o644); err != nil {
		t.Fatalf("write malformed file failed: %v", err)
	}

	logger := drslog.NewNoOpLogger()
	objects, err := GetDrsLfsObjects(logger)
	if err != nil {
		t.Fatalf("GetDrsLfsObjects error: %v", err)
	}
	if len(objects) != 1 {
		t.Fatalf("expected 1 drs object, got %d", len(objects))
	}
	if _, ok := objects["sha-256-value"]; !ok {
		t.Fatalf("expected sha-256-value key")
	}
}
