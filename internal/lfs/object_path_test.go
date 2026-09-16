package lfs

import "testing"

func TestObjectPathRejectsInvalidOID(t *testing.T) {
	path, err := ObjectPath(".git/lfs/objects", "short")
	if err == nil {
		t.Fatalf("expected validation error, got %s", path)
	}
}

func TestObjectPathAcceptsDRSPointerOIDValue(t *testing.T) {
	fullURIPath, err := ObjectPath(".git/drs/objects", "drs://cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c")
	if err != nil {
		t.Fatalf("expected DRS URI object path: %v", err)
	}
	pointerOIDPath, err := ObjectPath(".git/drs/objects", "//cgc-ga4gh-api.sbgenomics.com/4c33ae65e4b08832ce3d94e9c")
	if err != nil {
		t.Fatalf("expected DRS pointer oid object path: %v", err)
	}
	if pointerOIDPath != fullURIPath {
		t.Fatalf("expected DRS pointer oid and full URI to produce same path, got %q and %q", pointerOIDPath, fullURIPath)
	}
}
