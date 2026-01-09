package indexd_client

import (
	"testing"

	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
)

func TestIndexdRecordConversions(t *testing.T) {
	accessMethods := []drs.AccessMethod{
		{
			AccessURL: drs.AccessURL{URL: "s3://bucket/key"},
			Authorizations: &drs.Authorizations{
				Value: "/programs/test/projects/proj",
			},
		},
	}
	input := &drs.DRSObject{
		Id:            "did-1",
		Name:          "file.txt",
		Size:          123,
		AccessMethods: accessMethods,
		Checksums:     hash.HashInfo{SHA256: "sha-256"},
	}

	indexdRecord, err := indexdRecordFromDrsObject(input)
	if err != nil {
		t.Fatalf("indexdRecordFromDrsObject error: %v", err)
	}
	if indexdRecord.Did != input.Id || indexdRecord.FileName != input.Name {
		t.Fatalf("indexd record mismatch: %+v", indexdRecord)
	}
	if len(indexdRecord.URLs) != 1 || indexdRecord.URLs[0] != "s3://bucket/key" {
		t.Fatalf("unexpected URLs: %+v", indexdRecord.URLs)
	}
	if len(indexdRecord.Authz) != 1 || indexdRecord.Authz[0] != "/programs/test/projects/proj" {
		t.Fatalf("unexpected authz: %+v", indexdRecord.Authz)
	}

	out, err := indexdRecordToDrsObject(indexdRecord)
	if err != nil {
		t.Fatalf("indexdRecordToDrsObject error: %v", err)
	}
	if out.Id != input.Id || out.Name != input.Name || out.Size != input.Size {
		t.Fatalf("drs object mismatch: %+v", out)
	}
	if len(out.AccessMethods) != 1 || out.AccessMethods[0].AccessURL.URL != "s3://bucket/key" {
		t.Fatalf("unexpected access methods: %+v", out.AccessMethods)
	}
}

func TestDrsAccessMethodsFromIndexdURLs_AuthzRequired(t *testing.T) {
	if _, err := drsAccessMethodsFromIndexdURLs([]string{"s3://bucket/key"}, nil); err == nil {
		t.Fatalf("expected authz error")
	}
}
