package lfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
	drsapi "github.com/calypr/syfon/apigen/drs"
)

func TestReachablePointersPreserveRepositoryRootWhitespace(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo ")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repo, "init", "-q")
	gitTestCommand(t, repo, "config", "user.email", "test@example.com")
	gitTestCommand(t, repo, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(repo, "data.bin"), []byte("version https://git-lfs.github.com/spec/v1\noid sha256:"+strings.Repeat("a", 64)+"\nsize 12\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTestCommand(t, repo, "add", "data.bin")
	gitTestCommand(t, repo, "commit", "-qm", "add pointer")

	files, err := GetReachablePointerFilesForRefInRepository(context.Background(), repo, "HEAD", drslog.NewNoOpLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["data.bin"]; !ok {
		t.Fatalf("pointer missing from repository with trailing space: %+v", files)
	}
}

func TestGetLfsFilesForRefsPreservesNewlinePath(t *testing.T) {
	repo := t.TempDir()
	gitTestCommand(t, repo, "init", "-q")
	gitTestCommand(t, repo, "config", "user.email", "test@example.com")
	gitTestCommand(t, repo, "config", "user.name", "test")
	for i, name := range []string{"normal.bin", "line\nbreak.bin"} {
		id := strings.Repeat("a", 64)
		if i == 1 {
			id = strings.Repeat("b", 64)
		}
		pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:" + id + "\nsize 12\n"
		if err := os.WriteFile(filepath.Join(repo, name), []byte(pointer), 0o644); err != nil {
			t.Fatal(err)
		}
		gitTestCommand(t, repo, "add", name)
	}
	gitTestCommand(t, repo, "commit", "-qm", "add pointers")
	gitTestCommand(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	t.Chdir(repo)

	files, err := GetLfsFilesForRefs([]string{"refs/remotes/origin/main"}, drslog.NewNoOpLogger())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"normal.bin", "line\nbreak.bin"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("missing %q from ref pointer inventory: %+v", name, files)
		}
	}
}

func TestCreateDRSPointerPreservesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reference")
	const original = "existing user data\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	obj := &drsapi.DrsObject{Size: 42}
	if err := CreateDRSPointer(obj, path, "drs://example.org/object-1"); err == nil {
		t.Fatal("existing destination was overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("existing file changed: %q", got)
	}
	if err := CreateDRSPointer(obj, filepath.Join(filepath.Dir(path), "new-reference"), "drs://example.org/object-1"); err != nil {
		t.Fatalf("new destination rejected: %v", err)
	}
}

func TestParsePlaceholderPointer(t *testing.T) {
	oid := strings.Repeat("a", 64)
	pointer, ok := parseLFSPointer("version https://git-lfs.github.com/spec/v1\next-0-gitdrsplaceholder sha256:" + oid + "\noid sha256:" + oid + "\nsize 7\n")
	if !ok || !pointer.Placeholder || pointer.Oid != oid {
		t.Fatalf("pointer = %+v, ok = %v", pointer, ok)
	}
}

func TestParseLFSPointerRejectsUnknownSHA256Version(t *testing.T) {
	oid := strings.Repeat("a", 64)
	if _, ok := parseLFSPointer("version https://example.invalid/spec/v1\noid sha256:" + oid + "\nsize 7\n"); ok {
		t.Fatal("parseLFSPointer accepted a SHA-256 pointer with an unknown version")
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

func TestGetTrackedLfsFilesPreservesLeadingSpaceInPath(t *testing.T) {
	repo := t.TempDir()
	runGitCmdTest(t, repo, "init")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.dat filter=drs\n"), 0o644); err != nil {
		t.Fatalf("write attributes: %v", err)
	}
	path := " lead.dat"
	oid := strings.Repeat("a", 64)
	writePointerFile(t, filepath.Join(repo, path), oid, "4")
	runGitCmdTest(t, repo, "add", "--", ".gitattributes", path)

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	files, err := GetTrackedLfsFiles(drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("GetTrackedLfsFiles: %v", err)
	}
	if info, ok := files[path]; !ok || info.Oid != oid {
		t.Fatalf("tracked path %q missing or incorrect: %+v", path, files)
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
