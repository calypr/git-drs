package query

import (
	"context"
	"testing"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
)

type fakeChecksumClient struct {
	received *hash.Checksum
	objects  []drs.DRSObject
	err      error
}

func (f *fakeChecksumClient) GetObjectByHash(ctx context.Context, sum *hash.Checksum) ([]drs.DRSObject, error) {
	f.received = sum
	return f.objects, f.err
}

func TestQueryByChecksumTypes(t *testing.T) {
	client := &fakeChecksumClient{}

	tests := []struct {
		hash     string
		expected hash.ChecksumType
	}{
		{"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hash.ChecksumTypeSHA256},
		{"d41d8cd98f00b204e9800998ecf8427e", hash.ChecksumTypeMD5},
		{"da39a3ee5e6b4b0d3255bfef95601890afd80709", hash.ChecksumTypeSHA1},
		{"cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e", hash.ChecksumTypeSHA512},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			_, _ = queryByChecksum(client, tt.hash)
			if client.received.Type != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, client.received.Type)
			}
		})
	}
}

func TestPrintDRSObject(t *testing.T) {
	obj := drs.DRSObject{Id: "test-id"}
	err := printDRSObject(obj, false)
	if err != nil {
		t.Errorf("printDRSObject failed: %v", err)
	}
	err = printDRSObject(obj, true)
	if err != nil {
		t.Errorf("printDRSObject pretty failed: %v", err)
	}
}
