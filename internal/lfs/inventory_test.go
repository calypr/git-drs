package lfs

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
)

func TestGetLfsFilesForRefPaths(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "checkout", "-b", "main")

	oid := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	pointerPath := filepath.Join(repo, "data", "main-pointer.dat")
	writePointerFile(t, pointerPath, oid, "123")
	regularPath := filepath.Join(repo, "data", "regular.txt")
	if err := os.WriteFile(regularPath, []byte("not a pointer"), 0o644); err != nil {
		t.Fatalf("write regular file: %v", err)
	}
	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "main commit")

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
	files, err := GetLfsFilesForRefPaths("HEAD", []string{"data/main-pointer.dat", "data/regular.txt", "data/missing.bin"}, logger)
	if err != nil {
		t.Fatalf("GetLfsFilesForRefPaths error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one pointer file, got %+v", files)
	}
	info, ok := files["data/main-pointer.dat"]
	if !ok {
		t.Fatalf("missing pointer file in result: %+v", files)
	}
	if info.Oid != oid || info.Size != 123 || !info.IsPointer {
		t.Fatalf("unexpected pointer info: %+v", info)
	}
}

func TestGetLfsFilesForRefPathsHandlesSpaces(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "checkout", "-b", "main")

	oid := "997312a8a4f826fd4e4ef2d572badacea37a3dc79e87336a97a4c0f82ef25f14"
	spacePath := "data/BigMHC Training and Evaluation Data/el_test.csv"
	writePointerFile(t, filepath.Join(repo, filepath.FromSlash(spacePath)), oid, "102550733")
	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit pointer with spaces")

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

	files, err := GetLfsFilesForRefPaths("HEAD", []string{spacePath}, drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("GetLfsFilesForRefPaths error: %v", err)
	}
	info, ok := files[spacePath]
	if !ok {
		t.Fatalf("missing pointer file with spaces in result: %+v", files)
	}
	if info.Oid != oid || info.Size != 102550733 || !info.IsPointer {
		t.Fatalf("unexpected pointer info: %+v", info)
	}
}

func TestGetReachablePointerFilesForRefHandlesSpacesAndIgnoresNonPointers(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "checkout", "-b", "main")

	oid := "997312a8a4f826fd4e4ef2d572badacea37a3dc79e87336a97a4c0f82ef25f14"
	spacePath := "data/BigMHC Training and Evaluation Data/el_test.csv"
	writePointerFile(t, filepath.Join(repo, filepath.FromSlash(spacePath)), oid, "102550733")
	if err := os.WriteFile(filepath.Join(repo, "data", "regular.txt"), []byte("regular content"), 0o644); err != nil {
		t.Fatalf("write regular file: %v", err)
	}
	malformedPath := filepath.Join(repo, "data", "malformed.dat")
	if err := os.WriteFile(malformedPath, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:not-a-sha\nsize 10\n"), 0o644); err != nil {
		t.Fatalf("write malformed pointer: %v", err)
	}
	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit reachable files")

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

	files, err := GetReachablePointerFilesForRef("HEAD", drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("GetReachablePointerFilesForRef error: %v", err)
	}
	info, ok := files[spacePath]
	if !ok {
		t.Fatalf("missing pointer file with spaces in reachable result: %+v", files)
	}
	if info.Oid != oid || info.Size != 102550733 || !info.IsPointer {
		t.Fatalf("unexpected pointer info: %+v", info)
	}
	if _, ok := files["data/regular.txt"]; ok {
		t.Fatalf("regular file should not be included: %+v", files)
	}
	if _, ok := files["data/malformed.dat"]; ok {
		t.Fatalf("malformed pointer should not be included: %+v", files)
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

func TestGetTrackedLfsFiles_IncludesHydratedTrackedFileUsingIndexPointer(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	runGitCmdTest(t, repo, "config", "user.email", "test@example.com")
	runGitCmdTest(t, repo, "config", "user.name", "Test User")
	runGitCmdTest(t, repo, "config", "filter.drs.clean", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.smudge", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.process", "cat")
	runGitCmdTest(t, repo, "config", "filter.drs.required", "false")

	attrPath := filepath.Join(repo, ".gitattributes")
	if err := os.WriteFile(attrPath, []byte("*.dat filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}

	oid := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	pointerPath := filepath.Join(repo, "data", "hydrated.dat")
	writePointerFile(t, pointerPath, oid, "321")

	runGitCmdTest(t, repo, "add", ".")
	runGitCmdTest(t, repo, "commit", "-m", "commit tracked pointer")

	if err := os.WriteFile(pointerPath, []byte("localized payload"), 0o644); err != nil {
		t.Fatalf("hydrate tracked file: %v", err)
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
	files, err := GetTrackedLfsFiles(logger)
	if err != nil {
		t.Fatalf("GetTrackedLfsFiles error: %v", err)
	}

	info, ok := files["data/hydrated.dat"]
	if !ok {
		t.Fatalf("expected hydrated tracked file in inventory")
	}
	if info.Oid != oid || info.Size != 321 {
		t.Fatalf("unexpected hydrated tracked info: %+v", info)
	}
	if info.IsPointer {
		t.Fatalf("expected hydrated tracked file to be marked non-pointer in worktree inventory")
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

	attr := []byte("*.dat filter=drs diff=drs merge=drs -text\n")
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
