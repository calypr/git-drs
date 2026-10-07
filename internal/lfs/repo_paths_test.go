package lfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveLFSRoot_ConfigTildeExpansion(t *testing.T) {
	// This test relies on `sh -lc` in userHomeDir, which we don't run on Windows.
	if runtime.GOOS == "windows" {
		t.Skip("tilde expansion test skipped on windows")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	ctx := context.Background()
	repo := t.TempDir()
	home := filepath.Join(repo, "fake-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir fake home: %v", err)
	}

	// Force HOME so userHomeDir() resolves consistently
	oldHome := os.Getenv("HOME")
	_ = os.Setenv("HOME", home)
	t.Cleanup(func() { _ = os.Setenv("HOME", oldHome) })

	mustRun(t, repo, "git", "init")

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	mustRun(t, repo, "git", "config", "lfs.storage", "~/lfs-store")

	gitCommon, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		t.Fatalf("gitRevParseGitCommonDir: %v", err)
	}

	lfsRoot, err := resolveLFSRoot(ctx, gitCommon)
	if err != nil {
		t.Fatalf("resolveLFSRoot: %v", err)
	}

	want := filepath.Clean(filepath.Join(home, "lfs-store"))
	if lfsRoot != want {
		t.Fatalf("expected %q, got %q", want, lfsRoot)
	}
}

func TestGitCommonDirAndResolveLFSRoot_Default(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	ctx := context.Background()
	repo := t.TempDir()

	mustRun(t, repo, "git", "init")
	// ensure we're in that repo for git config calls
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	gitCommon, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		t.Fatalf("gitRevParseGitCommonDir: %v", err)
	}

	lfsRoot, err := resolveLFSRoot(ctx, gitCommon)
	if err != nil {
		t.Fatalf("resolveLFSRoot: %v", err)
	}

	want := filepath.Clean(filepath.Join(gitCommon, "lfs"))
	if lfsRoot != want {
		t.Fatalf("expected lfsRoot %q, got %q", want, lfsRoot)
	}
}

func TestResolveLFSRoot_ConfigAbsolute(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	ctx := context.Background()
	repo := t.TempDir()
	absStorage := filepath.Join(repo, "custom-lfs-storage")

	mustRun(t, repo, "git", "init")

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	// set lfs.storage
	mustRun(t, repo, "git", "config", "lfs.storage", absStorage)

	gitCommon, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		t.Fatalf("gitRevParseGitCommonDir: %v", err)
	}

	lfsRoot, err := resolveLFSRoot(ctx, gitCommon)
	if err != nil {
		t.Fatalf("resolveLFSRoot: %v", err)
	}

	want := filepath.Clean(absStorage)
	if lfsRoot != want {
		t.Fatalf("expected %q, got %q", want, lfsRoot)
	}
}

func TestResolveLFSRoot_ConfigRelative(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	ctx := context.Background()
	repo := t.TempDir()
	mustRun(t, repo, "git", "init")

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	// relative storage path (resolved under gitCommonDir in our helper)
	mustRun(t, repo, "git", "config", "lfs.storage", "rel-lfs")

	gitCommon, err := gitRevParseGitCommonDir(ctx)
	if err != nil {
		t.Fatalf("gitRevParseGitCommonDir: %v", err)
	}

	lfsRoot, err := resolveLFSRoot(ctx, gitCommon)
	if err != nil {
		t.Fatalf("resolveLFSRoot: %v", err)
	}

	want := filepath.Clean(filepath.Join(gitCommon, "rel-lfs"))
	if lfsRoot != want {
		t.Fatalf("expected %q, got %q", want, lfsRoot)
	}
}

func TestGetGitRootDirectoriesFromRepositorySubdirectory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	repo := t.TempDir()
	mustRun(t, repo, "git", "init")
	canonicalGitDir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatalf("resolve temporary git directory path: %v", err)
	}
	subdir := filepath.Join(repo, "nested", "package")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir repository subdirectory: %v", err)
	}

	oldwd := mustChdir(t, subdir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	gitCommonDir, lfsRoot, err := GetGitRootDirectories(context.Background())
	if err != nil {
		t.Fatalf("GetGitRootDirectories from subdirectory: %v", err)
	}
	if want := canonicalGitDir; gitCommonDir != want {
		t.Errorf("git common dir: want %q, got %q", want, gitCommonDir)
	}
	if want := filepath.Join(canonicalGitDir, "lfs"); lfsRoot != want {
		t.Errorf("LFS root: want %q, got %q", want, lfsRoot)
	}
}

func TestGetGitRootDirectoriesFromWorktreeSubdirectoryWithConfiguredStorage(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found in PATH")
	}

	repo := t.TempDir()
	mustRun(t, repo, "git", "init")
	canonicalGitDir, err := filepath.EvalSymlinks(filepath.Join(repo, ".git"))
	if err != nil {
		t.Fatalf("resolve temporary git directory path: %v", err)
	}
	mustRun(t, repo, "git", "config", "user.name", "git-drs test")
	mustRun(t, repo, "git", "config", "user.email", "git-drs-test@example.invalid")
	mustRun(t, repo, "git", "config", "lfs.storage", "shared-lfs")
	mustRun(t, repo, "git", "-c", "user.name=git-drs test", "-c", "user.email=git-drs-test@example.invalid", "commit", "--allow-empty", "-m", "initial")

	worktree := filepath.Join(t.TempDir(), "linked-worktree")
	mustRun(t, repo, "git", "worktree", "add", worktree, "HEAD")
	subdir := filepath.Join(worktree, "nested", "package")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("mkdir worktree subdirectory: %v", err)
	}

	oldwd := mustChdir(t, subdir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	gitCommonDir, lfsRoot, err := GetGitRootDirectories(context.Background())
	if err != nil {
		t.Fatalf("GetGitRootDirectories from linked worktree subdirectory: %v", err)
	}
	if want := canonicalGitDir; gitCommonDir != want {
		t.Errorf("git common dir: want shared directory %q, got %q", want, gitCommonDir)
	}
	if want := filepath.Join(canonicalGitDir, "shared-lfs"); lfsRoot != want {
		t.Errorf("LFS root: want configured path under shared git directory %q, got %q", want, lfsRoot)
	}
}

// --- test helpers ---

func mustRun(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %s %v\nerr=%v\nout=%s", name, args, err, string(out))
	}
}

func mustChdir(t *testing.T, dir string) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return old
}
