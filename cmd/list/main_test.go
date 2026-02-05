package list

import (
	"testing"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
)

func TestGetChecksumPos(t *testing.T) {
	if pos := getChecksumPos(hash.ChecksumTypeSHA256, checksumPref); pos != 0 {
		t.Fatalf("expected SHA256 at pos 0, got %d", pos)
	}
	if pos := getChecksumPos(hash.ChecksumType("missing"), checksumPref); pos != -1 {
		t.Fatalf("expected missing checksum -1, got %d", pos)
	}
}

func TestGetCheckSumStr(t *testing.T) {
	obj := drs.DRSObject{Checksums: hash.HashInfo{MD5: "md5", SHA256: "sha"}}
	value := getCheckSumStr(obj)
	if value != "sha256:sha" {
		t.Fatalf("unexpected checksum string: %s", value)
	}
}
