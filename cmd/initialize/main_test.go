package initialize

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/drsobject"
	internalfilter "github.com/calypr/git-drs/internal/filter"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/testutils"
)

func TestInstallPreCommitHook(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	logger := drslog.NewNoOpLogger()

	if err := installPreCommitHook(logger); err != nil {
		t.Fatalf("installPreCommitHook error: %v", err)
	}

	hookPath := filepath.Join(".git", "hooks", "pre-commit")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(content), "git drs precommit") {
		t.Fatalf("expected hook to contain git drs precommit")
	}

	if err := installPreCommitHook(logger); err != nil {
		t.Fatalf("installPreCommitHook second call error: %v", err)
	}
}

func TestInitGitConfig(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	transfers = 2
	if err := initGitConfig(); err != nil {
		t.Fatalf("initGitConfig error: %v", err)
	}
}
func TestInitRun_Error(t *testing.T) {
	// Not in a git repo
	tmpDir := t.TempDir()
	cwd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(cwd)

	err := Cmd.RunE(Cmd, []string{})
	if err == nil {
		t.Errorf("expected error when not in git repo")
	}
}
func TestInitCmdArgs(t *testing.T) {
	err := Cmd.Args(Cmd, []string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	err = Cmd.Args(Cmd, []string{"extra"})
	if err == nil {
		t.Errorf("expected error for extra args")
	}
}
func TestInitConfigValues(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	transfers = 8

	if err := initGitConfig(); err != nil {
		t.Fatalf("initGitConfig error: %v", err)
	}

	// Verify values using gitrepo (which we know works from previous steps)
	check := func(key, expected string) {
		val, err := gitrepo.GetGitConfigString(key)
		if err != nil {
			t.Errorf("error reading %s: %v", key, err)
		}
		if val != expected {
			t.Errorf("expected %s to be %s, got %s", key, expected, val)
		}
	}

	check("lfs.concurrenttransfers", "8")
	check("lfs.allowincompletepush", "false")
	check("push.autoSetupRemote", "true")
	check("filter.drs.clean", "git-drs clean -- %f")
	check("filter.drs.smudge", "git-drs smudge -- %f")
	check("filter.drs.process", "git-drs filter")
	check("filter.drs.required", "true")
}

func TestEnsureInitialized(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	logger := drslog.NewNoOpLogger()

	if err := EnsureInitialized(logger); err != nil {
		t.Fatalf("EnsureInitialized error: %v", err)
	}
	if err := EnsureInitialized(logger); err != nil {
		t.Fatalf("EnsureInitialized second call error: %v", err)
	}

	if _, err := os.Stat(gitrepo.DRSDir); err != nil {
		t.Fatalf("expected %s to exist: %v", gitrepo.DRSDir, err)
	}
	filterProcess, err := gitrepo.GetGitConfigString("filter.drs.process")
	if err != nil {
		t.Fatalf("GetGitConfigString(filter.drs.process): %v", err)
	}
	if filterProcess != "git-drs filter" {
		t.Fatalf("unexpected filter.drs.process: %q", filterProcess)
	}
	filterClean, err := gitrepo.GetGitConfigString("filter.drs.clean")
	if err != nil {
		t.Fatalf("GetGitConfigString(filter.drs.clean): %v", err)
	}
	if filterClean != "git-drs clean -- %f" {
		t.Fatalf("unexpected filter.drs.clean: %q", filterClean)
	}
}

func TestEnsureInitializedRemovesLegacyPrePushHook(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	logger := drslog.NewNoOpLogger()

	hookPath := filepath.Join(".git", "hooks", "pre-push")
	legacyHook := "#!/bin/sh\nexec git drs pre-push-prepare\n"
	if err := os.WriteFile(hookPath, []byte(legacyHook), 0o755); err != nil {
		t.Fatalf("write legacy pre-push hook: %v", err)
	}

	if err := EnsureInitialized(logger); err != nil {
		t.Fatalf("EnsureInitialized error: %v", err)
	}

	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("expected legacy pre-push hook to be removed, got err=%v", err)
	}

	matches, err := filepath.Glob(hookPath + ".*")
	if err != nil {
		t.Fatalf("glob hook backups: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected legacy pre-push hook backup to be created")
	}
}

func TestInitializeRepoInLinkedWorktree(t *testing.T) {
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	mainRepo := filepath.Join(root, "main")
	linkedRepo := filepath.Join(root, "linked")
	if err := os.MkdirAll(mainRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitInTest(t, mainRepo, "init")
	runGitInTest(t, mainRepo, "config", "user.email", "test@example.com")
	runGitInTest(t, mainRepo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(mainRepo, "README"), []byte("linked worktree test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitInTest(t, mainRepo, "add", "README")
	runGitInTest(t, mainRepo, "commit", "-m", "initial")
	runGitInTest(t, mainRepo, "worktree", "add", "-b", "linked", linkedRepo, "HEAD")
	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
	})
	if err := os.Chdir(linkedRepo); err != nil {
		t.Fatal(err)
	}

	logger, err := drslog.NewLogger("", false)
	if err != nil {
		t.Fatalf("open linked-worktree logger: %v", err)
	}
	t.Cleanup(func() {
		if err := drslog.Close(); err != nil {
			t.Errorf("close logger: %v", err)
		}
	})
	if err := InitializeRepo(logger); err != nil {
		t.Fatalf("InitializeRepo in linked worktree: %v", err)
	}

	gitDir := gitOutputInTest(t, linkedRepo, "rev-parse", "--absolute-git-dir")
	commonDir := filepath.Join(mainRepo, ".git")
	hooksDir := gitOutputInTest(t, linkedRepo, "rev-parse", "--path-format=absolute", "--git-path", "hooks")
	if _, err := os.Stat(filepath.Join(gitDir, "drs", "git-drs.log")); err != nil {
		t.Fatalf("linked-worktree log file: %v", err)
	}
	hookPath := filepath.Join(hooksDir, "pre-commit")
	if content, err := os.ReadFile(hookPath); err != nil || !strings.Contains(string(content), "git drs precommit") {
		t.Fatalf("pre-commit hook at Git hooks path %q: content=%q err=%v", hookPath, content, err)
	}
	if _, err := os.Stat(filepath.Join(commonDir, "drs", "lfs", "objects")); err != nil {
		t.Fatalf("shared DRS object directory: %v", err)
	}

	_, lfsRoot, err := lfs.GetGitRootDirectories(context.Background())
	if err != nil {
		t.Fatalf("resolve linked-worktree LFS root: %v", err)
	}
	var pointer bytes.Buffer
	payload := []byte("linked worktree payload")
	if err := internalfilter.CleanContent(context.Background(), lfsRoot, "payload.bin", bytes.NewReader(payload), &pointer, logger); err != nil {
		t.Fatalf("clean linked-worktree payload: %v", err)
	}
	oid, _, ok := lfs.ParseLFSPointer(pointer.Bytes())
	if !ok {
		t.Fatalf("clean produced invalid pointer: %q", pointer.String())
	}
	objectsRoot := filepath.Join(commonDir, "drs", "lfs", "objects")
	if _, err := drsobject.ReadObject(objectsRoot, oid); err != nil {
		t.Fatalf("read pointer metadata from shared object store: %v", err)
	}
	cachePath, err := lfs.ObjectPath(filepath.Join(commonDir, "lfs", "objects"), oid)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(cachePath); err != nil || info.Size() != int64(len(payload)) {
		t.Fatalf("read cached payload %q: info=%v err=%v", cachePath, info, err)
	}
}

func runGitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutputInTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(output))
}
