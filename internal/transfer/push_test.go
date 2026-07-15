package transfer

import (
	"testing"

	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestBatchSyncSessionNormalizeFilesDeduplicatesByOID(t *testing.T) {
	session := &batchSyncSession{}
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	session.normalizeFiles(map[string]lfs.LfsFileInfo{
		"data/a.dat": {
			Name: "data/a.dat",
			Oid:  oid,
		},
		"data/b.dat": {
			Name: "data/b.dat",
			Oid:  oid,
		},
		"data/c.dat": {
			Name: "data/c.dat",
			Oid:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	})

	if len(session.filesByOID) != 2 {
		t.Fatalf("expected two unique oids, got %+v", session.filesByOID)
	}
	if len(session.oids) != 2 {
		t.Fatalf("expected two sorted oids, got %+v", session.oids)
	}
	if _, ok := session.filesByOID[oid]; !ok {
		t.Fatalf("missing normalized oid %s in %+v", oid, session.filesByOID)
	}
}

func TestAddURLObjectRegistersWithoutLocalPayloadUpload(t *testing.T) {
	t.Chdir(t.TempDir())
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	accessMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/external/object"},
	}}
	obj := &drsapi.DrsObject{
		Checksums:     []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
		AccessMethods: &accessMethods,
	}
	if err := localdrsobject.WriteObject(gitrepo.DRSObjectsPath, obj, oid); err != nil {
		t.Fatalf("write add-url object: %v", err)
	}
	if !localObjectHasResolvableAccessMethod(oid) {
		t.Fatal("expected local add-url metadata to provide a resolvable external payload")
	}

	session := &batchSyncSession{
		rt:             &pushRuntime{},
		uploadRequired: map[string]bool{oid: false},
		existingByHash: map[string][]drsapi.DrsObject{oid: nil},
	}
	needsUpload, err := session.needsUpload(oid)
	if err != nil {
		t.Fatalf("needsUpload: %v", err)
	}
	if needsUpload {
		t.Fatal("add-url metadata must be registered without scheduling a local payload upload")
	}
}
