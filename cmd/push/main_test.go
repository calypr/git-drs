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

func TestCurrentPushRefUpdatesUsesZeroBaseWhenUpstreamMissing(t *testing.T) {
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

	got, err := currentPushRefUpdates(context.Background())
	if err != nil {
		t.Fatalf("currentPushRefUpdates returned error: %v", err)
	}
	if len(got) != 1 || got[0].OldSHA != "0000000000000000000000000000000000000000" || got[0].NewSHA != "head-sha" {
		t.Fatalf("unexpected push refs: %+v", got)
	}
}

func TestListRefUpdatePathsUsesDiffForExistingBranch(t *testing.T) {
	oldFn := gitOutputFn
	gitOutputFn = func(ctx context.Context, args ...string) (string, error) {
		if fmt.Sprint(args) != "[diff --name-only old-sha new-sha]" {
			t.Fatalf("unexpected git args: %v", args)
		}
		return "a.dat\nb.txt\n", nil
	}
	t.Cleanup(func() { gitOutputFn = oldFn })

	got, err := listRefUpdatePaths(context.Background(), []drsdelete.RefUpdate{{OldSHA: "old-sha", NewSHA: "new-sha"}})
	if err != nil {
		t.Fatalf("listRefUpdatePaths returned error: %v", err)
	}
	if len(got) != 2 || got[0] != "a.dat" || got[1] != "b.txt" {
		t.Fatalf("unexpected paths: %+v", got)
	}
}

func TestListRefUpdatePathsUsesLsTreeForFirstPush(t *testing.T) {
	oldFn := gitOutputFn
	gitOutputFn = func(ctx context.Context, args ...string) (string, error) {
		if fmt.Sprint(args) != "[ls-tree -r --name-only new-sha]" {
			t.Fatalf("unexpected git args: %v", args)
		}
		return "a.dat\nb.txt\n", nil
	}
	t.Cleanup(func() { gitOutputFn = oldFn })

	got, err := listRefUpdatePaths(context.Background(), []drsdelete.RefUpdate{{OldSHA: "0000000000000000000000000000000000000000", NewSHA: "new-sha"}})
	if err != nil {
		t.Fatalf("listRefUpdatePaths returned error: %v", err)
	}
	if len(got) != 2 || got[0] != "a.dat" || got[1] != "b.txt" {
		t.Fatalf("unexpected paths: %+v", got)
	}
}
