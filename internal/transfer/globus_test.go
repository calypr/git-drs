package transfer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseGlobusURL(t *testing.T) {
	loc, err := parseGlobusURL("globus://01234567-89ab-cdef-0123-456789abcdef/data/sample.bam")
	if err != nil {
		t.Fatalf("parseGlobusURL returned error: %v", err)
	}
	if loc.Collection != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Fatalf("collection = %q", loc.Collection)
	}
	if loc.Path != "/data/sample.bam" {
		t.Fatalf("path = %q", loc.Path)
	}
}

func TestParseGlobusURLRejectsMissingPath(t *testing.T) {
	if _, err := parseGlobusURL("globus://collection-only"); err == nil {
		t.Fatal("expected missing path to fail")
	}
}

func TestGlobusDestinationForCachePathRequiresCollection(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "")
	if _, err := globusDestinationForCachePath(filepath.Join(".git", "drs", "objects", "aa")); err == nil {
		t.Fatal("expected missing destination collection to fail")
	}
}

func TestGlobusDestinationForCachePathUsesLFSCachePath(t *testing.T) {
	t.Setenv(globusDestCollectionEnv, "dest-collection")
	loc, err := globusDestinationForCachePath(filepath.Join(".git", "lfs", "objects", "aa", "bb"))
	if err != nil {
		t.Fatalf("globusDestinationForCachePath returned error: %v", err)
	}
	if loc.Collection != "dest-collection" {
		t.Fatalf("collection = %q", loc.Collection)
	}
	wantSuffix := "/.git/lfs/objects/aa/bb"
	if loc.Path != wantSuffix {
		t.Fatalf("path = %q, want %q", loc.Path, wantSuffix)
	}
}

func TestIsGlobusURL(t *testing.T) {
	if !isGlobusURL("globus://collection/path") {
		t.Fatal("expected globus URL")
	}
	if isGlobusURL("https://example.test/path") || isGlobusURL(os.DevNull) {
		t.Fatal("expected non-globus URL to be rejected")
	}
}
