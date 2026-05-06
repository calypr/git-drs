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

func TestGetWorktreeLfsFiles(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")

	oid := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	pointerPath := filepath.Join(repo, "data", "pointer.dat")
	writePointerFile(t, pointerPath, oid, "789")

	localizedPath := filepath.Join(repo, "data", "localized.bin")
	if err := os.WriteFile(localizedPath, []byte("hydrated"), 0o644); err != nil {
		t.Fatalf("write localized file: %v", err)
	}

	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit pointer")

	if err := os.WriteFile(pointerPath, []byte("hydrated pointer replacement"), 0o644); err != nil {
		t.Fatalf("replace pointer with hydrated content: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	logger := drslog.NewNoOpLogger()
	files, err := GetWorktreeLfsFiles(logger)
	if err != nil {
		t.Fatalf("GetWorktreeLfsFiles error: %v", err)
	}
	if _, exists := files["data/pointer.dat"]; exists {
		t.Fatalf("hydrated file should not still appear as a pointer")
	}

	if err := os.WriteFile(pointerPath, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:"+oid+"\nsize 789\n"), 0o644); err != nil {
		t.Fatalf("restore pointer file: %v", err)
	}

	files, err = GetWorktreeLfsFiles(logger)
	if err != nil {
		t.Fatalf("GetWorktreeLfsFiles error after restore: %v", err)
	}
	info, ok := files["data/pointer.dat"]
	if !ok {
		t.Fatalf("expected pointer in worktree inventory")
	}
	if info.Oid != oid || info.Size != 789 {
		t.Fatalf("unexpected pointer info: %+v", info)
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

func TestIsLFSTracked(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	repo := t.TempDir()
	mustRun(t, repo, "git", "init")

	attr := []byte("*.dat filter=lfs diff=lfs merge=lfs -text\n")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), attr, 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	tracked := filepath.Join(repo, "data", "file.dat")
	untracked := filepath.Join(repo, "data", "file.txt")
	if err := os.MkdirAll(filepath.Dir(tracked), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(tracked, []byte("x"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	if err := os.WriteFile(untracked, []byte("y"), 0o644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	got, err := IsLFSTracked("data/file.dat")
	if err != nil {
		t.Fatalf("IsLFSTracked tracked: %v", err)
	}
	if !got {
		t.Fatalf("expected data/file.dat to be LFS tracked")
	}

	got, err = IsLFSTracked("data/file.txt")
	if err != nil {
		t.Fatalf("IsLFSTracked untracked: %v", err)
	}
	if got {
		t.Fatalf("expected data/file.txt to NOT be LFS tracked")
	}
}
