package lfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/gitrepo"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestObjectWalk(t *testing.T) {
	setupTempRepo(t)
	baseDir := filepath.Join(".git", "drs", "objects")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	name := "object-name"
	obj := drsapi.DrsObject{
		Id:   "object-1",
		Name: ptrString(name),
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: "sha-256"},
		},
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
	err = ObjectWalk(func(path string, d *drsapi.DrsObject) error {
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
	t.Skip("temporarily disabled TODO - fix git attributes handling in tests")
}

func TestDrsTopLevel(t *testing.T) {
	tmp := t.TempDir()
	drsDir := filepath.Join(tmp, ".git", "drs")
	if err := os.MkdirAll(drsDir, 0755); err != nil {
		t.Fatalf("mkdir .git/drs: %v", err)
	}

	cmd := exec.Command("git", "-C", tmp, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, string(out))
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

func ptrString(s string) *string { return &s }

func setupTempRepo(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	cmd := exec.Command("git", "init", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
