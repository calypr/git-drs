package push

import "testing"

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
