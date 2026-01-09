package drsmap

import (
	"context"
	"os"
	"os/exec"
	"testing"

	"github.com/calypr/git-drs/drslog"
)

func TestResolveRemoteAndRef_Defaults(t *testing.T) {
	tmp := t.TempDir()
	cmd := exec.Command("git", "init", tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}

	logger := drslog.NewNoOpLogger()
	spec, err := ResolveRemoteAndRef(context.Background(), tmp, "origin", logger)
	if err != nil {
		t.Fatalf("ResolveRemoteAndRef error: %v", err)
	}
	if spec.Remote != "origin" {
		t.Fatalf("expected origin remote, got %s", spec.Remote)
	}
	if spec.Ref != "HEAD" {
		t.Fatalf("expected HEAD ref, got %s", spec.Ref)
	}
}

func TestRunGit_Error(t *testing.T) {
	logger := drslog.NewNoOpLogger()
	if _, err := runGit(context.Background(), os.TempDir(), logger, "unknown-command"); err == nil {
		t.Fatalf("expected error for bad git command")
	}
}
