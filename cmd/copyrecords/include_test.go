package copyrecords

import (
	"log/slog"
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
)

func TestIncludedLocalPaths(t *testing.T) {
	include, err := normalizeIncludedPaths([]string{"./META/", "CONFIG"})
	if err != nil {
		t.Fatalf("normalizeIncludedPaths: %v", err)
	}
	tests := map[string]bool{
		"META":               true,
		"META/manifest.json": true,
		"CONFIG/dev.yaml":    true,
		"CONFIGURATION/x":    false,
		"data/file.bin":      false,
	}
	for path, want := range tests {
		if got := isIncludedLocalPath(path, include); got != want {
			t.Errorf("isIncludedLocalPath(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestCopyRecordMatchesIncludedSHA256(t *testing.T) {
	rec := copyRecord{Hashes: &copyHashInfo{"sha256": "ABC123"}}
	if !copyRecordMatchesIncludedSHA256(rec, map[string]struct{}{"abc123": {}}) {
		t.Fatal("expected record checksum to match local allowlist")
	}
	if copyRecordMatchesIncludedSHA256(rec, map[string]struct{}{"different": {}}) {
		t.Fatal("did not expect different checksum to match")
	}
}

func TestIncludedLocalSHA256UsesRepositoryPaths(t *testing.T) {
	oldLoad := loadTrackedLfsFiles
	t.Cleanup(func() { loadTrackedLfsFiles = oldLoad })
	loadTrackedLfsFiles = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"META/Condition.ndjson": {Oid: "sha256:aaaa"},
			"CONFIG/settings.json":  {Oid: "sha256:bbbb"},
			"DATA/unrelated.ndjson": {Oid: "sha256:cccc"},
		}, nil
	}
	hashes, err := includedLocalSHA256([]string{"META", "CONFIG"})
	if err != nil {
		t.Fatalf("includedLocalSHA256: %v", err)
	}
	if len(hashes) != 2 {
		t.Fatalf("expected two hashes, got %#v", hashes)
	}
	if _, ok := hashes["aaaa"]; !ok {
		t.Fatal("META checksum missing")
	}
	if _, ok := hashes["bbbb"]; !ok {
		t.Fatal("CONFIG checksum missing")
	}
}

func TestNormalizeIncludedPathsRejectsPathsOutsideRepository(t *testing.T) {
	for _, path := range []string{"../META", "/META", "."} {
		if _, err := normalizeIncludedPaths([]string{path}); err == nil {
			t.Errorf("expected %q to be rejected", path)
		}
	}
}
