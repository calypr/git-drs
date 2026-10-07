package push

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRemoteSyncRefs(t *testing.T) {
	gotRemote, gotAck, err := readRemoteSyncRefsOutput(
		"1111111111111111111111111111111111111111\trefs/heads/main\n"+
			"2222222222222222222222222222222222222222\trefs/git-drs/synced/heads/main\n",
		"refs/heads/main",
		"refs/git-drs/synced/heads/main",
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotRemote != "1111111111111111111111111111111111111111" || gotAck != "2222222222222222222222222222222222222222" {
		t.Fatalf("unexpected refs: remote=%s ack=%s", gotRemote, gotAck)
	}
}

func TestPushSyncAcknowledgmentSkipsAdvanceWhenObjectsWereUnavailable(t *testing.T) {
	old := gitOutputFn
	gitOutputFn = func(context.Context, ...string) (string, error) {
		t.Fatal("synchronization acknowledgement was advanced")
		return "", nil
	}
	t.Cleanup(func() { gitOutputFn = old })

	if err := pushSyncAcknowledgment(context.Background(), "origin", syncRefState{}, 1); err != nil {
		t.Fatal(err)
	}
}

func TestPushRefspecUsesCapturedTargetOID(t *testing.T) {
	state := syncRefState{LocalRef: "refs/heads/main", RemoteRef: "refs/heads/main", TargetOID: strings.Repeat("a", 40)}
	if got, want := pushRefspec(state), strings.Repeat("a", 40)+":refs/heads/main"; got != want {
		t.Fatalf("push refspec = %q, want %q", got, want)
	}
}

func TestPushRefspecPublishesCapturedCommitAfterBranchAdvances(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	remote := filepath.Join(t.TempDir(), "remote.git")
	runPushGit(t, "", "init", "--bare", remote)
	runPushGit(t, "", "init", repo)
	runPushGit(t, repo, "config", "user.email", "test@example.com")
	runPushGit(t, repo, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repo, "data.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPushGit(t, repo, "add", "data.txt")
	runPushGit(t, repo, "commit", "-m", "A")
	captured := strings.TrimSpace(runPushGit(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "data.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	runPushGit(t, repo, "commit", "-am", "B")
	advanced := strings.TrimSpace(runPushGit(t, repo, "rev-parse", "HEAD"))
	if captured == advanced {
		t.Fatal("branch did not advance")
	}

	state := syncRefState{LocalRef: "refs/heads/main", RemoteRef: "refs/heads/main", TargetOID: captured}
	runPushGit(t, repo, "push", remote, pushRefspec(state))
	published := strings.TrimSpace(runPushGit(t, "", "--git-dir", remote, "rev-parse", "refs/heads/main"))
	if published != captured {
		t.Fatalf("published OID = %s, want captured OID %s (advanced branch is %s)", published, captured, advanced)
	}
}

func runPushGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}
