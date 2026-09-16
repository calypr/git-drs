package push

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
	internaltransfer "github.com/calypr/git-drs/internal/transfer"
)

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

	oldMbFn := getRemoteMergeBaseFn
	getRemoteMergeBaseFn = func(ctx context.Context, remote string, head string) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { getRemoteMergeBaseFn = oldMbFn })

	got, err := currentPushRefUpdates(context.Background(), "origin")
	if err != nil {
		t.Fatalf("currentPushRefUpdates returned error: %v", err)
	}
	if len(got) != 1 || got[0].OldSHA != "0000000000000000000000000000000000000000" || got[0].NewSHA != "head-sha" {
		t.Fatalf("unexpected push refs: %+v", got)
	}
}

func TestCurrentPushRefUpdatesUsesRemoteMergeBaseWhenUpstreamMissing(t *testing.T) {
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

	oldMbFn := getRemoteMergeBaseFn
	getRemoteMergeBaseFn = func(ctx context.Context, remote string, head string) (string, error) {
		if remote != "origin" || head != "head-sha" {
			t.Fatalf("unexpected getRemoteMergeBase args: remote=%s, head=%s", remote, head)
		}
		return "merge-base-sha", nil
	}
	t.Cleanup(func() { getRemoteMergeBaseFn = oldMbFn })

	got, err := currentPushRefUpdates(context.Background(), "origin")
	if err != nil {
		t.Fatalf("currentPushRefUpdates returned error: %v", err)
	}
	if len(got) != 1 || got[0].OldSHA != "merge-base-sha" || got[0].NewSHA != "head-sha" {
		t.Fatalf("unexpected push refs: %+v", got)
	}
}

func TestDiscoverLfsFilesForPushScansReachableNewSHA(t *testing.T) {
	oldReachableFn := getReachablePointerFilesForRefFn
	t.Cleanup(func() {
		getReachablePointerFilesForRefFn = oldReachableFn
	})

	getReachablePointerFilesForRefFn = func(ref string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		if ref != "new-sha" {
			t.Fatalf("unexpected reachable ref scan: %s", ref)
		}
		return map[string]lfs.LfsFileInfo{"data/file.dat": {Name: "data/file.dat"}}, nil
	}

	got, err := discoverLfsFilesForPush([]internaltransfer.RefUpdate{{OldSHA: "old-sha", NewSHA: "new-sha"}}, slog.Default())
	if err != nil {
		t.Fatalf("discoverLfsFilesForPush returned error: %v", err)
	}
	if _, ok := got["data/file.dat"]; !ok {
		t.Fatalf("missing discovered file: %+v", got)
	}
}

func TestDiscoverLfsFilesForPushScansReachableNewSHAWhenDiffWouldBeEmpty(t *testing.T) {
	oldReachableFn := getReachablePointerFilesForRefFn
	t.Cleanup(func() {
		getReachablePointerFilesForRefFn = oldReachableFn
	})

	getReachablePointerFilesForRefFn = func(ref string, logger *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		if ref != "HEAD" {
			t.Fatalf("unexpected reachable ref scan: %s", ref)
		}
		return map[string]lfs.LfsFileInfo{
			"data/BigMHC Training and Evaluation Data/el_test.csv": {
				Name: "data/BigMHC Training and Evaluation Data/el_test.csv",
			},
		}, nil
	}

	got, err := discoverLfsFilesForPush([]internaltransfer.RefUpdate{{OldSHA: "HEAD", NewSHA: "HEAD"}}, slog.Default())
	if err != nil {
		t.Fatalf("discoverLfsFilesForPush returned error: %v", err)
	}
	if _, ok := got["data/BigMHC Training and Evaluation Data/el_test.csv"]; !ok {
		t.Fatalf("missing fallback discovered file: %+v", got)
	}
}

func TestCountUniqueOIDsDeduplicates(t *testing.T) {
	got := countUniqueOIDs(map[string]lfs.LfsFileInfo{
		"data/a.dat": {Oid: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		"data/b.dat": {Oid: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		"data/c.dat": {Oid: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	})
	if got != 2 {
		t.Fatalf("expected two unique oids, got %d", got)
	}
}

func TestIncludeReachablePlaceholders(t *testing.T) {
	files := map[string]lfs.LfsFileInfo{"data/new.dat": {Oid: "new"}}
	includeReachablePlaceholders(files, map[string]lfs.LfsFileInfo{
		"data/external.dat": {Oid: "placeholder", Placeholder: true},
		"data/local.dat":    {Oid: "local"},
	})
	if _, ok := files["data/external.dat"]; !ok {
		t.Fatal("reachable placeholder was not scheduled for metadata synchronization")
	}
	if _, ok := files["data/local.dat"]; ok {
		t.Fatal("unchanged non-placeholder was scheduled")
	}
}
