package transfer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	localdrsobject "github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	internalapi "github.com/calypr/syfon/apigen/internalapi"
	syclient "github.com/calypr/syfon/client"
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

func TestAddURLObjectDoesNotProduceUploadCandidate(t *testing.T) {
	t.Chdir(t.TempDir())
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	accessMethods := []drsapi.AccessMethod{{
		Type:      drsapi.AccessMethodTypeS3,
		AccessUrl: &drsapi.AccessURL{Url: "s3://bucket/external/object"},
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
		oids:           []string{oid},
		filesByOID:     map[string]lfs.LfsFileInfo{oid: {Oid: oid, Name: "data/external.dat"}},
		uploadRequired: map[string]bool{oid: false},
	}
	candidates, err := session.identifyUploadCandidates()
	if err != nil {
		t.Fatalf("identifyUploadCandidates: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("add-url metadata scheduled upload candidates: %+v", candidates)
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

func TestClonedPlaceholderKeepsRemoteMetadata(t *testing.T) {
	t.Chdir(t.TempDir())
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	realOID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	controlled := []string{"/organization/example/project/tutorial"}
	methods := []drsapi.AccessMethod{{
		Type:      drsapi.AccessMethodTypeGlobus,
		AccessUrl: &drsapi.AccessURL{Url: "globus://source/data/file.dat"},
	}}
	match := drsapi.DrsObject{
		Id:               "remote-object",
		Size:             10,
		Checksums:        []drsapi.Checksum{{Type: "git-drs-placeholder", Checksum: oid}, {Type: "sha256", Checksum: realOID}},
		ControlledAccess: &controlled,
		AccessMethods:    &methods,
	}
	session := &batchSyncSession{
		rt:             &pushRuntime{Scope: pushScope{Organization: "example", Project: "tutorial", Bucket: "target"}},
		filesByOID:     map[string]lfs.LfsFileInfo{oid: {Oid: oid, Name: "data/file.dat", Size: 10, Placeholder: true}},
		oids:           []string{oid},
		drsObjByOID:    make(map[string]*drsapi.DrsObject),
		existingByHash: map[string][]drsapi.DrsObject{oid: {match}},
		uploadRequired: make(map[string]bool),
	}
	if err := session.ensureMetadataRegistered(); err != nil {
		t.Fatal(err)
	}
	if got := session.drsObjByOID[oid]; got == nil || got.Id != match.Id || firstAccessURL(got) != firstAccessURL(&match) {
		t.Fatalf("remote placeholder metadata was not retained: %+v", got)
	}
}

func TestPushLookupFindsReusableRecordOutsideTargetScope(t *testing.T) {
	missingOID := strings.Repeat("a", 64)
	presentOID := strings.Repeat("b", 64)
	controlled := []string{"/organization/other/project/source"}
	methods := []drsapi.AccessMethod{{
		Type:      drsapi.AccessMethodTypeS3,
		AccessUrl: &drsapi.AccessURL{Url: "s3://source-bucket/object"},
	}}
	hashes := internalapi.HashInfo{"sha256": missingOID}
	var globalQueries [][]string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/index/bulk/sha256/missing":
			var request internalapi.BulkMissingSHA256Request
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode scoped request: %v", err)
				return
			}
			if request.Organization != "org" || request.Project != "target" || !reflect.DeepEqual(request.Sha256, []string{missingOID, presentOID}) {
				t.Errorf("scoped request = %+v", request)
			}
			_, _ = w.Write([]byte(`{"checked":2,"missing_sha256":["` + missingOID + `"]}`))
		case "/index/bulk/hashes":
			var request internalapi.BulkHashesRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode global request: %v", err)
				return
			}
			globalQueries = append(globalQueries, request.Hashes)
			if err := json.NewEncoder(w).Encode(struct {
				Results map[string][]internalapi.InternalRecord `json:"results"`
			}{Results: map[string][]internalapi.InternalRecord{
				missingOID: {{Did: "reusable", ControlledAccess: &controlled, AccessMethods: &methods, Hashes: &hashes}},
			}}); err != nil {
				t.Errorf("encode global response: %v", err)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	})

	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		return response.Result(), nil
	})}
	client, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	session := &batchSyncSession{
		ctx:  t.Context(),
		rt:   &pushRuntime{API: &remoteruntime.GitContext{Client: client, Organization: "org", ProjectId: "target"}, Scope: pushScope{Organization: "org", Project: "target"}},
		oids: []string{missingOID, presentOID},
	}
	if err := session.lookupMetadata(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(globalQueries, [][]string{{missingOID}}) {
		t.Fatalf("global queries = %v, want only the OID missing in target scope", globalQueries)
	}
	if session.presentInScope[missingOID] || !session.presentInScope[presentOID] {
		t.Fatalf("scope presence = %v", session.presentInScope)
	}
	if reusable := session.findReusableRecord(session.existingByHash[missingOID]); reusable == nil || firstAccessURL(reusable) != "s3://source-bucket/object" {
		t.Fatalf("missing OID has no reusable source record: %+v", reusable)
	}
}
