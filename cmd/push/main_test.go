package push

import (
	"context"
	"fmt"
	"testing"

	"github.com/calypr/git-drs/internal/drsdelete"
)

func TestCurrentDeleteRefUpdatesUsesUpstreamWhenConfigured(t *testing.T) {
	oldFn := gitOutputFn
	gitOutputFn = func(ctx context.Context, args ...string) (string, error) {
		switch fmt.Sprint(args) {
		case "[rev-parse HEAD]":
			return "head-sha", nil
		case "[rev-parse --verify @{upstream}]":
			return "upstream-sha", nil
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}
	t.Cleanup(func() { gitOutputFn = oldFn })

	got, err := currentDeleteRefUpdates(context.Background())
	if err != nil {
		t.Fatalf("currentDeleteRefUpdates returned error: %v", err)
	}
	want := []drsdelete.RefUpdate{{OldSHA: "upstream-sha", NewSHA: "head-sha"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("unexpected delete refs: got %+v want %+v", got, want)
	}
}

func TestCurrentDeleteRefUpdatesSkipsWhenUpstreamMissing(t *testing.T) {
	oldFn := gitOutputFn
	gitOutputFn = func(ctx context.Context, args ...string) (string, error) {
		switch fmt.Sprint(args) {
		case "[rev-parse HEAD]":
			return "head-sha", nil
		case "[rev-parse --verify @{upstream}]":
			return "", fmt.Errorf("git rev-parse --verify @{upstream}: fatal: no upstream configured")
		default:
			t.Fatalf("unexpected git args: %v", args)
			return "", nil
		}
	}
	t.Cleanup(func() { gitOutputFn = oldFn })

	got, err := currentDeleteRefUpdates(context.Background())
	if err != nil {
		t.Fatalf("currentDeleteRefUpdates returned error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil delete refs when upstream is missing, got %+v", got)
	}
}
