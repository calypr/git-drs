package lfs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/utils"
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

func TestIsLFSTrackedFile(t *testing.T) {
	tmp := t.TempDir()
	attrPath := filepath.Join(tmp, ".gitattributes")
	err := os.WriteFile(attrPath, []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0644)
	if err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	tests := []struct {
		path    string
		tracked bool
	}{
		{"test.bin", true},
		{"sub/test.bin", true},
		{"test.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			tracked, err := IsLFSTracked(attrPath, tt.path)
			if err != nil {
				t.Fatalf("IsLFSTracked error: %v", err)
			}
			if tracked != tt.tracked {
				t.Errorf("IsLFSTracked(%s) = %v, want %v", tt.path, tracked, tt.tracked)
			}
		})
	}
}

func TestDrsTopLevel(t *testing.T) {
	tmp := t.TempDir()
	drsDir := filepath.Join(tmp, ".git", "drs")
	err := os.MkdirAll(drsDir, 0755)
	if err != nil {
		t.Fatalf("mkdir .git/drs: %v", err)
	}

	// Initialize git repo so git commands work
	_, err = utils.SimpleRun([]string{"git", "-C", tmp, "init"})
	if err != nil {
		t.Fatalf("git init: %v", err)
	}

	cwd, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(cwd)

	top, err := gitrepo.DrsTopLevel()
	if err != nil {
		t.Fatalf("DrsTopLevel error: %v", err)
	}

	expected, _ := filepath.EvalSymlinks(drsDir)
	actual, _ := filepath.EvalSymlinks(top)
	if actual != expected {
		t.Errorf("expected %s, got %s", expected, actual)
	}
}
