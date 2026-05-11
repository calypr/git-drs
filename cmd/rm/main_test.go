package rm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunRemovesTrackedFile(t *testing.T) {
	repo := t.TempDir()
	runGitCmd(t, repo, "init")
	runGitCmd(t, repo, "config", "user.email", "test@example.com")
	runGitCmd(t, repo, "config", "user.name", "Test User")
	runGitCmd(t, repo, "config", "filter.drs.clean", "cat")
	runGitCmd(t, repo, "config", "filter.drs.smudge", "cat")
	runGitCmd(t, repo, "config", "filter.drs.process", "cat")
	runGitCmd(t, repo, "config", "filter.drs.required", "false")

	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.dat filter=drs diff=drs merge=drs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	path := filepath.Join(repo, "data.dat")
	if err := os.WriteFile(path, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nsize 12\n"), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
	runGitCmd(t, repo, "add", ".")
	runGitCmd(t, repo, "commit", "-m", "add pointer")

	oldWD, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if err := run(context.Background(), []string{"data.dat"}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file removed from worktree, stat err=%v", err)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
