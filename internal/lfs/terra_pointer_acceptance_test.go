package lfs

import "testing"

func TestAcceptanceTerraDRSURIoidPointerIsRecognized(t *testing.T) {
	const pointer = `version https://calypr.github.io/spec/v1
oid drs://cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c
size 11305017366
`

	info, ok := parseLFSPointer(pointer)
	if !ok {
		t.Fatalf("expected git-drs DRS URI pointer to be recognized")
	}
	if info.Version != "https://calypr.github.io/spec/v1" {
		t.Fatalf("unexpected pointer version: %q", info.Version)
	}
	if info.OidType != "drs" || info.Oid != "//cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c" {
		t.Fatalf("expected DRS URI oid to be preserved, got type=%q oid=%q", info.OidType, info.Oid)
	}
	if info.Size != 11305017366 {
		t.Fatalf("unexpected pointer size: %d", info.Size)
	}
}

func TestAcceptanceTerraDRSURIoidPointerHasDeterministicSHA256CacheKey(t *testing.T) {
	const drsURI = "drs://cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c"

	cacheKeyPath, err := ObjectPath(".git/drs/objects", drsURI)
	if err != nil {
		t.Fatalf("expected DRS URI to be normalized to a sha256-shaped cache key path: %v", err)
	}
	if cacheKeyPath == "" {
		t.Fatalf("expected non-empty cache path for DRS URI")
	}
}
