package query

import (
	"testing"

	"github.com/calypr/git-drs/internal/common"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestChecksumTypeForString(t *testing.T) {
	tests := []struct {
		sum  string
		want string
	}{
		{"d41d8cd98f00b204e9800998ecf8427e", "md5"},
		{"da39a3ee5e6b4b0d3255bfef95601890afd80709", "sha1"},
		{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "sha256"},
		{"cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e", "sha512"},
		{" sha256 ", "sha256"},
	}
	for _, tt := range tests {
		if got := checksumTypeForString(tt.sum); got != tt.want {
			t.Fatalf("checksumTypeForString(%q) = %q, want %q", tt.sum, got, tt.want)
		}
	}
}

func TestPrintDRSObject(t *testing.T) {
	obj := drsapi.DrsObject{Id: "test-id"}
	if err := common.PrintDRSObject(obj, false); err != nil {
		t.Fatalf("common.PrintDRSObject failed: %v", err)
	}
	if err := common.PrintDRSObject(obj, true); err != nil {
		t.Fatalf("common.PrintDRSObject pretty failed: %v", err)
	}
}
