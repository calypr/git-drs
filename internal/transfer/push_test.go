package transfer

import (
	"testing"

	"github.com/calypr/git-drs/internal/lfs"
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
