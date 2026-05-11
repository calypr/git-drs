package pushsync

import (
	"os"
	"path/filepath"
	"testing"

	localcommon "github.com/calypr/git-drs/internal/common"
)

func TestResolveUploadSourcePath_NoSentinelObjectForPointer(t *testing.T) {
	repo := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	worktreePath := filepath.Join(repo, "data.bin")
	if err := os.WriteFile(worktreePath, []byte("version https://git-lfs.github.com/spec/v1\noid sha256:"+oid+"\nsize 1\n"), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}

	// Ensure the implicit cache root matches command behavior under the cwd.
	if _, err := os.Stat(localcommon.LFS_OBJS_PATH); err == nil {
		t.Fatalf("expected no local object cache at %s", localcommon.LFS_OBJS_PATH)
	}

	src, ok, err := resolveUploadSourcePath(oid, worktreePath, true)
	if err != nil {
		t.Fatalf("resolveUploadSourcePath: %v", err)
	}
	if ok || src != "" {
		t.Fatalf("expected pointer without local payload to skip upload source, got src=%q ok=%v", src, ok)
	}
}
