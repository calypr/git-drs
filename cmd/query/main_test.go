package query

import (
	"testing"

	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
)

type fakeChecksumClient struct {
	received *hash.Checksum
	objects  []drs.DRSObject
	err      error
}

func (f *fakeChecksumClient) GetObjectByHash(sum *hash.Checksum) ([]drs.DRSObject, error) {
	f.received = sum
	return f.objects, f.err
}

func TestQueryByChecksumUsesSHA256(t *testing.T) {
	client := &fakeChecksumClient{
		objects: []drs.DRSObject{{Id: "drs-object-1"}},
	}

	objs, err := queryByChecksum(client, "abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.received == nil {
		t.Fatalf("expected checksum request to be recorded")
	}
	if client.received.Type != hash.ChecksumTypeSHA256 {
		t.Fatalf("expected checksum type sha256, got %s", client.received.Type)
	}
	if client.received.Checksum != "abc123" {
		t.Fatalf("expected checksum value abc123, got %s", client.received.Checksum)
	}
	if len(objs) != 1 || objs[0].Id != "drs-object-1" {
		t.Fatalf("unexpected objects returned: %+v", objs)
	}
}
