package initialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/internal/testutils"
)

func TestInstallPrePushHook(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	logger := drslog.NewNoOpLogger()

	if err := installPrePushHook(logger); err != nil {
		t.Fatalf("installPrePushHook error: %v", err)
	}

	hookPath := filepath.Join(".git", "hooks", "pre-push")
	content, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(content), "git drs pre-push") {
		t.Fatalf("expected hook to contain git drs pre-push")
	}

	if err := installPrePushHook(logger); err != nil {
		t.Fatalf("installPrePushHook second call error: %v", err)
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
