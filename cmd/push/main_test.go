package push

import (
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
)

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
