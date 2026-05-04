package lfs

import "testing"

func TestObjectPathRejectsInvalidOID(t *testing.T) {
	path, err := ObjectPath(".git/lfs/objects", "short")
	if err == nil {
		t.Fatalf("expected validation error, got %s", path)
	}
}
