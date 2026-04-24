package lfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
)

func TestGetAllLfsFilesFromGitRefsWithoutLfsCli(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "checkout", "-b", "main")

	oidMain := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	mainPointerPath := filepath.Join(repo, "data", "main-pointer.dat")
	writePointerFile(t, mainPointerPath, oidMain, "123")

	nonPointerPath := filepath.Join(repo, "data", "regular.txt")
	if err := os.MkdirAll(filepath.Dir(nonPointerPath), 0o755); err != nil {
		t.Fatalf("mkdir non-pointer dir: %v", err)
	}
	if err := os.WriteFile(nonPointerPath, []byte("not an lfs pointer"), 0o644); err != nil {
		t.Fatalf("write non-pointer file: %v", err)
	}

	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "main commit")

	runGitCmdTest(t, repo, "checkout", "-b", "feature")
	oidFeature := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	featurePointerPath := filepath.Join(repo, "feature", "space name.bin")
	writePointerFile(t, featurePointerPath, oidFeature, "456")
	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "feature commit")

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	logger := drslog.NewNoOpLogger()
	files, err := GetAllLfsFiles("origin", "", []string{"main", "feature"}, logger)
	if err != nil {
		t.Fatalf("GetAllLfsFiles error: %v", err)
	}

	mainInfo, ok := files["data/main-pointer.dat"]
	if !ok {
		t.Fatalf("missing main pointer in result")
	}
	if mainInfo.Oid != oidMain {
		t.Fatalf("main pointer oid mismatch: expected %s, got %s", oidMain, mainInfo.Oid)
	}
	if mainInfo.Size != 123 {
		t.Fatalf("main pointer size mismatch: expected 123, got %d", mainInfo.Size)
	}
	if !mainInfo.IsPointer {
		t.Fatalf("main pointer IsPointer should be true")
	}

	featureInfo, ok := files["feature/space name.bin"]
	if !ok {
		t.Fatalf("missing feature pointer in result")
	}
	if featureInfo.Oid != oidFeature {
		t.Fatalf("feature pointer oid mismatch: expected %s, got %s", oidFeature, featureInfo.Oid)
	}
	if featureInfo.Size != 456 {
		t.Fatalf("feature pointer size mismatch: expected 456, got %d", featureInfo.Size)
	}
	if !featureInfo.IsPointer {
		t.Fatalf("feature pointer IsPointer should be true")
	}

	if _, exists := files["data/regular.txt"]; exists {
		t.Fatalf("non-pointer file should not be returned")
	}
}

func writePointerFile(t *testing.T, path, oid, size string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	content := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + oid + "\n" +
		"size " + size + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
}

func runGitCmdTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
