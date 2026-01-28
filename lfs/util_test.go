package lfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
)

func TestObjectWalk(t *testing.T) {
	setupTempRepo(t)
	baseDir := filepath.Join(".git", "drs", "objects")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	obj := drs.DRSObject{
		Id:        "object-1",
		Name:      "object-name",
		Checksums: hash.HashInfo{SHA256: "sha-256"},
	}
	data, err := sonic.ConfigFastest.Marshal(obj)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	filePath := filepath.Join(baseDir, "item.json")
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	var seenPath string
	var seenID string
	err = ObjectWalk(func(path string, d *drs.DRSObject) error {
		seenPath = path
		if d != nil {
			seenID = d.Id
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ObjectWalk error: %v", err)
	}
	if seenPath != filepath.Join("objects", "item.json") {
		t.Fatalf("unexpected path %s", seenPath)
	}
	if seenID != "object-1" {
		t.Fatalf("unexpected id %s", seenID)
	}
}
