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
