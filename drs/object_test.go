package drs

import (
	"reflect"
	"testing"

	"github.com/calypr/git-drs/drs/hash"
)

func TestConvertOutputObjectToDRSObject(t *testing.T) {
	input := &OutputObject{
		Id:          "object-1",
		Name:        "object-name",
		SelfURI:     "drs://example/object-1",
		Size:        42,
		CreatedTime: "2024-01-01T00:00:00Z",
		Checksums: []hash.Checksum{
			{Checksum: "sha-256", Type: hash.ChecksumTypeSHA256},
			{Checksum: "md5-value", Type: hash.ChecksumTypeMD5},
		},
		AccessMethods: []AccessMethod{{Type: "https"}},
		Description:   "example",
		Aliases:       []string{"alias-1"},
	}

	got := ConvertOutputObjectToDRSObject(input)
	if got == nil {
		t.Fatalf("expected non-nil object")
	}
	if got.Id != input.Id || got.Name != input.Name || got.Size != input.Size {
		t.Fatalf("field mismatch: %+v", got)
	}
	if got.Checksums.SHA256 != "sha-256" || got.Checksums.MD5 != "md5-value" {
		t.Fatalf("checksum conversion mismatch: %+v", got.Checksums)
	}
	if !reflect.DeepEqual(got.Aliases, input.Aliases) {
		t.Fatalf("aliases mismatch: %+v", got.Aliases)
	}
}

func TestConvertOutputObjectToDRSObjectNil(t *testing.T) {
	if ConvertOutputObjectToDRSObject(nil) != nil {
		t.Fatalf("expected nil result")
	}
}
