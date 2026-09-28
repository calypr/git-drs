package copyrecords

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/drs"
)

func TestLocalSourceFromSubdirectoryIncludesRepositoryFiles(t *testing.T) {
	root := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	write := func(name, oid string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + oid + "\nsize 1\n"
		if err := os.WriteFile(path, []byte(pointer), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("*.bin filter=drs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write("outside.bin", strings.Repeat("a", 64))
	write("sub/inside.bin", strings.Repeat("b", 64))
	cmd = exec.Command("git", "add", ".gitattributes", "outside.bin", "sub/inside.bin")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}

	oldRead := readLocalDRSObject
	readLocalDRSObject = func(oid string) (*drsapi.DrsObject, error) {
		return &drsapi.DrsObject{Id: oid, Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}}, nil
	}
	t.Cleanup(func() { readLocalDRSObject = oldRead })
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(root, "sub")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	records, err := loadLocalSource(context.Background(), "Org", "Proj")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("local source from subdirectory returned %d records, want both repository records", len(records))
	}
}
