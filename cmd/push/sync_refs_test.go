package push

import (
	"context"
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
