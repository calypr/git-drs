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

func TestBatchSyncSessionNormalizeFilesExcludesDRSURIReferences(t *testing.T) {
	session := &batchSyncSession{}
	checksum := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	session.normalizeFiles(map[string]lfs.LfsFileInfo{
		"data/checksum.dat": {
			Name: "data/checksum.dat",
			Oid:  checksum,
		},
		"data/parsed-reference.dat": {
			Name: "data/parsed-reference.dat",
			Oid:  "//authority.example/object-with-sha256",
		},
		"data/canonical-reference.dat": {
			Name: "data/canonical-reference.dat",
			Oid:  "drs://authority.example/another-object",
		},
	})

	if len(session.oids) != 1 || session.oids[0] != checksum {
		t.Fatalf("expected only checksum OID in push synchronization, got %+v", session.oids)
	}
	if len(session.filesByOID) != 1 {
		t.Fatalf("expected only checksum file in push synchronization, got %+v", session.filesByOID)
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

func TestPlaceholderMetadataUsesTemporaryChecksumType(t *testing.T) {
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	realOID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	session := &batchSyncSession{
		rt:         &pushRuntime{Scope: pushScope{Organization: "example", Project: "tutorial"}},
		filesByOID: map[string]lfs.LfsFileInfo{oid: {Oid: oid, Placeholder: true}},
	}
	obj, err := scopedDRSObjectForPush(session.rt, oid, "data/file.dat", 10, &drsapi.DrsObject{
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: realOID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := session.metadataRecordForOID(oid, obj)
	if record.Hashes == nil || (*record.Hashes)["git-drs-placeholder"] != oid {
		t.Fatalf("placeholder hashes = %+v", record.Hashes)
	}
	if (*record.Hashes)["sha256"] != realOID {
		t.Fatalf("learned sha256 was not preserved: %+v", *record.Hashes)
	}
	temporary := session.metadataRecordForOID(oid, &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}}})
	if _, mislabeled := (*temporary.Hashes)["sha256"]; mislabeled {
		t.Fatalf("temporary oid was labeled as a real sha256: %+v", *temporary.Hashes)
	}
	if !missingSHA256Checksum(obj, &drsapi.DrsObject{}) || missingSHA256Checksum(obj, &drsapi.DrsObject{Checksums: obj.Checksums}) {
		t.Fatal("learned sha256 synchronization detection failed")
	}
}
