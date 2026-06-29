package copyrecords

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
	"github.com/calypr/syfon/client/request"
	syservices "github.com/calypr/syfon/client/services"
)

type fakeIndexAPI struct {
	listResp      copyListRecordsResponse
	listFn        func(opts syservices.ListRecordsOptions) copyListRecordsResponse
	bulkDocsResp  []copyRecord
	bulkHashResp  copyBulkHashesResponse
	createBulkReq []copyBulkCreateRequest
}

func (f *fakeIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	if f.listFn != nil {
		return f.listFn(opts), nil
	}
	return f.listResp, nil
}

func (f *fakeIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	return f.bulkDocsResp, nil
}

func (f *fakeIndexAPI) BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error) {
	return f.bulkHashResp, nil
}

func (f *fakeIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	f.createBulkReq = append(f.createBulkReq, req)
	return copyListRecordsResponse{Records: &req.Records}, nil
}

func TestMergeExistingRecord_UnionsControlledAccessAndAccessMethodsOnly(t *testing.T) {
	dstName := "target-display"
	desc := "keep target description"
	leftCA := []string{"/organization/A/project/P1"}
	rightCA := []string{"/organization/A/project/P1", "/organization/A/project/P2"}
	leftMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/one"},
	}}
	rightMethods := []drsapi.AccessMethod{
		leftMethods[0],
		{
			Type: drsapi.AccessMethodTypeHttps,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: "https://example.org/two"},
		},
	}

	merged, changed := mergeExistingRecord(
		copyRecord{
			Did:              "did-1",
			Name:             &dstName,
			Description:      &desc,
			ControlledAccess: &leftCA,
			AccessMethods:    &leftMethods,
		},
		copyRecord{
			Did:              "did-1",
			ControlledAccess: &rightCA,
			AccessMethods:    &rightMethods,
		},
		false,
	)

	if !changed {
		t.Fatalf("expected merge to report a change")
	}
	if merged.Name == nil || *merged.Name != dstName {
		t.Fatalf("expected target metadata to be preserved, got %+v", merged.Name)
	}
	if merged.Description == nil || *merged.Description != desc {
		t.Fatalf("expected target description to be preserved")
	}
	if merged.ControlledAccess == nil || len(*merged.ControlledAccess) != 2 {
		t.Fatalf("expected merged controlled access union, got %+v", merged.ControlledAccess)
	}
	if merged.AccessMethods == nil || len(*merged.AccessMethods) != 2 {
		t.Fatalf("expected merged access method union, got %+v", merged.AccessMethods)
	}
}

func TestMergeExistingRecord_OverwritesNameWhenFlagEnabled(t *testing.T) {
	dstDisplayName := "target-display"
	srcDisplayName := "source-display"

	merged, changed := mergeExistingRecord(
		copyRecord{
			Did:  "did-1",
			Name: &dstDisplayName,
		},
		copyRecord{
			Did:  "did-1",
			Name: &srcDisplayName,
		},
		true,
	)

	if !changed {
		t.Fatalf("expected merge to report a change")
	}
	if merged.Name == nil || *merged.Name != srcDisplayName {
		t.Fatalf("expected source name to win, got %+v", merged.Name)
	}
}

func TestBuildMergedBatch_CreatesNewAndUpdatesExisting(t *testing.T) {
	srcCA := []string{"/organization/A/project/P1"}
	newCA := []string{"/organization/A/project/P2"}
	existingHash := copyHashInfo{"sha256": "sha-existing"}
	newHash := copyHashInfo{"sha256": "sha-new"}
	srcMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/a"},
	}}
	newMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeHttps,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "https://example.org/b"},
	}}

	target := &fakeIndexAPI{
		bulkDocsResp: []copyRecord{
			{
				Did:              "did-existing",
				Hashes:           &existingHash,
				ControlledAccess: &srcCA,
				AccessMethods:    &srcMethods,
			},
		},
		bulkHashResp: copyBulkHashesResponse{
			Results: map[string][]copyRecord{
				"sha256:sha-existing": {{
					Did:              "did-existing",
					Hashes:           &existingHash,
					ControlledAccess: &srcCA,
					AccessMethods:    &srcMethods,
				}},
			},
		},
	}

	source := []copyRecord{
		{
			Did:              "did-existing",
			Hashes:           &existingHash,
			ControlledAccess: &newCA,
			AccessMethods:    &newMethods,
		},
		{
			Did:              "did-new",
			Hashes:           &newHash,
			ControlledAccess: &srcCA,
			AccessMethods:    &srcMethods,
		},
	}

	out, stats, err := buildMergedBatch(context.Background(), target, source, false)
	if err != nil {
		t.Fatalf("buildMergedBatch error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 output records, got %d", len(out))
	}
	if stats.Created != 1 || stats.Updated != 1 || stats.Unchanged != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestBuildMergedBatch_MergesIntoExistingChecksumSiblingWhenDIDDiffers(t *testing.T) {
	srcHash := copyHashInfo{"sha256": "same-sha"}
	dstCA := []string{"/organization/A/project/P1"}
	srcCA := []string{"/organization/A/project/P2"}
	dstMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/existing"},
	}}
	srcMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeHttps,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "https://example.org/copied"},
	}}

	target := &fakeIndexAPI{
		bulkDocsResp: nil,
		bulkHashResp: copyBulkHashesResponse{
			Results: map[string][]copyRecord{
				"sha256:same-sha": {{
					Did:              "did-target",
					Hashes:           &srcHash,
					ControlledAccess: &dstCA,
					AccessMethods:    &dstMethods,
				}},
			},
		},
	}
	source := []copyRecord{{
		Did:              "did-source",
		Hashes:           &srcHash,
		ControlledAccess: &srcCA,
		AccessMethods:    &srcMethods,
	}}

	out, stats, err := buildMergedBatch(context.Background(), target, source, false)
	if err != nil {
		t.Fatalf("buildMergedBatch error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected one merged output record, got %d", len(out))
	}
	if out[0].Did != "did-target" {
		t.Fatalf("expected checksum sibling DID to be preserved, got %q", out[0].Did)
	}
	if out[0].ControlledAccess == nil || len(*out[0].ControlledAccess) != 2 {
		t.Fatalf("expected merged controlled access, got %+v", out[0].ControlledAccess)
	}
	if out[0].AccessMethods == nil || len(*out[0].AccessMethods) != 2 {
		t.Fatalf("expected merged access methods, got %+v", out[0].AccessMethods)
	}
	if stats.Created != 0 || stats.Updated != 1 || stats.Unchanged != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCopyProjectRecords_CopiesAllControlledAccessRecordsWhenScopedListIsEmpty(t *testing.T) {
	scopeCA := []string{"/organization/HTAN_INT/project/BForePC"}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			if opts.Organization == "HTAN_INT" && opts.ProjectID == "BForePC" {
				return copyListRecordsResponse{Records: &[]copyRecord{}}
			}
			if opts.Organization == "" && opts.ProjectID == "" && opts.Page == 1 {
				return copyListRecordsResponse{Records: &[]copyRecord{
					{Did: "did-in-scope", ControlledAccess: &scopeCA},
					{Did: "did-out-of-scope", ControlledAccess: &[]string{"/organization/OTHER/project/X"}},
				}}
			}
			return copyListRecordsResponse{Records: &[]copyRecord{}}
		},
	}
	target := &fakeIndexAPI{}

	records, err := listSourceRecordsByControlledAccess(context.Background(), source, "HTAN_INT", "BForePC", 100)
	if err != nil {
		t.Fatalf("listSourceRecordsByControlledAccess error: %v", err)
	}
	stats, err := copyProjectRecords(context.Background(), nil, records, target, "HTAN_INT", "BForePC", 100, false)
	if err != nil {
		t.Fatalf("copyProjectRecords error: %v", err)
	}
	if stats.SourceSeen != 1 || stats.Created != 1 || stats.Written != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(target.createBulkReq) != 1 || len(target.createBulkReq[0].Records) != 1 {
		t.Fatalf("expected one created record, got %+v", target.createBulkReq)
	}
	if target.createBulkReq[0].Records[0].Did != "did-in-scope" {
		t.Fatalf("unexpected copied did: %+v", target.createBulkReq[0].Records[0])
	}
}

func TestCopyProjectRecords_IgnoresPartialScopedListAndCopiesAllControlledAccessRecords(t *testing.T) {
	scopeCA := []string{"/organization/HTAN_INT/project/BForePC"}
	otherCA := []string{"/organization/OTHER/project/X"}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			if opts.Organization == "HTAN_INT" && opts.ProjectID == "BForePC" {
				return copyListRecordsResponse{Records: &[]copyRecord{
					{Did: "did-scoped-partial", ControlledAccess: &scopeCA},
				}}
			}
			if opts.Organization == "" && opts.ProjectID == "" && opts.Page == 1 {
				return copyListRecordsResponse{Records: &[]copyRecord{
					{Did: "did-scoped-partial", ControlledAccess: &scopeCA},
					{Did: "did-missing-from-scoped-list", ControlledAccess: &scopeCA},
					{Did: "did-out-of-scope", ControlledAccess: &otherCA},
				}}
			}
			return copyListRecordsResponse{Records: &[]copyRecord{}}
		},
	}
	target := &fakeIndexAPI{}

	records, err := listSourceRecordsByControlledAccess(context.Background(), source, "HTAN_INT", "BForePC", 100)
	if err != nil {
		t.Fatalf("listSourceRecordsByControlledAccess error: %v", err)
	}
	stats, err := copyProjectRecords(context.Background(), nil, records, target, "HTAN_INT", "BForePC", 100, false)
	if err != nil {
		t.Fatalf("copyProjectRecords error: %v", err)
	}
	if stats.SourceSeen != 2 || stats.Created != 2 || stats.Written != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if len(target.createBulkReq) != 1 || len(target.createBulkReq[0].Records) != 2 {
		t.Fatalf("expected two created records, got %+v", target.createBulkReq)
	}
	found := map[string]bool{}
	for _, rec := range target.createBulkReq[0].Records {
		found[rec.Did] = true
	}
	if !found["did-scoped-partial"] || !found["did-missing-from-scoped-list"] {
		t.Fatalf("missing copied records: %+v", target.createBulkReq[0].Records)
	}
	if found["did-out-of-scope"] {
		t.Fatalf("out-of-scope record should not be copied: %+v", target.createBulkReq[0].Records)
	}
}

func TestLoadLocalSourceRecords_DedupesTrackedOIDsAndRewritesScope(t *testing.T) {
	oldTracked := loadTrackedLfsFiles
	oldRead := readLocalDRSObject
	t.Cleanup(func() {
		loadTrackedLfsFiles = oldTracked
		readLocalDRSObject = oldRead
	})

	loadTrackedLfsFiles = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"data/a.bin": {Name: "data/a.bin", Oid: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 10},
			"data/b.bin": {Name: "data/b.bin", Oid: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 10},
		}, nil
	}
	loadCount := 0
	readLocalDRSObject = func(oid string) (*drsapi.DrsObject, error) {
		loadCount++
		name := "source.bin"
		methods := []drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: "s3://bucket/key"},
		}}
		return &drsapi.DrsObject{
			Id:            "did-1",
			Name:          &name,
			Checksums:     []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
			Size:          10,
			AccessMethods: &methods,
		}, nil
	}

	records, err := loadLocalSourceRecords("Org", "Proj")
	if err != nil {
		t.Fatalf("loadLocalSourceRecords error: %v", err)
	}
	if loadCount != 1 {
		t.Fatalf("expected one local object load, got %d", loadCount)
	}
	if len(records) != 1 {
		t.Fatalf("expected one deduped record, got %d", len(records))
	}
	rec := records[0]
	if rec.Organization == nil || *rec.Organization != "Org" {
		t.Fatalf("expected organization rewrite, got %+v", rec.Organization)
	}
	if rec.Project == nil || *rec.Project != "Proj" {
		t.Fatalf("expected project rewrite, got %+v", rec.Project)
	}
	if rec.ControlledAccess == nil || len(*rec.ControlledAccess) != 1 || (*rec.ControlledAccess)[0] != "/organization/Org/project/Proj" {
		t.Fatalf("expected rewritten controlled access, got %+v", rec.ControlledAccess)
	}
	if rec.AccessMethods == nil || len(*rec.AccessMethods) != 1 {
		t.Fatalf("expected access methods preserved, got %+v", rec.AccessMethods)
	}
}

func TestLoadLocalSourceRecords_MissingLocalObjectFailsClearly(t *testing.T) {
	oldTracked := loadTrackedLfsFiles
	oldRead := readLocalDRSObject
	t.Cleanup(func() {
		loadTrackedLfsFiles = oldTracked
		readLocalDRSObject = oldRead
	})

	loadTrackedLfsFiles = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{
			"data/a.bin": {Name: "data/a.bin", Oid: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Size: 10},
		}, nil
	}
	readLocalDRSObject = func(oid string) (*drsapi.DrsObject, error) {
		return nil, errors.New("not found")
	}

	_, err := loadLocalSourceRecords("Org", "Proj")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "tracked oid bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb for path data/a.bin is missing local DRS metadata") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCmdRunE_UsesLocalSourceWithoutBuildingSourceRuntime(t *testing.T) {
	oldLoadCfg := loadCopyConfig
	oldRuntime := newCopyRuntime
	oldLocal := loadLocalSource
	oldIndexAPI := newCopyIndexAPI
	t.Cleanup(func() {
		loadCopyConfig = oldLoadCfg
		newCopyRuntime = oldRuntime
		loadLocalSource = oldLocal
		newCopyIndexAPI = oldIndexAPI
	})

	loadCopyConfig = func() (*config.Config, error) {
		return &config.Config{
			DefaultRemote: "origin",
			Remotes: map[config.Remote]config.RemoteSelect{
				"target": {Local: &config.LocalRemote{BaseURL: "http://example.test", Organization: "Org", ProjectID: "Proj", Bucket: "bucket"}},
			},
		}, nil
	}
	runtimeCalls := 0
	newCopyRuntime = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		runtimeCalls++
		return &remoteruntime.GitContext{
			Client: &syclient.Client{},
		}, nil
	}
	loadLocalSource = func(ctx context.Context, org, project string) ([]copyRecord, error) {
		return []copyRecord{{
			Did:    "did-1",
			Hashes: &copyHashInfo{"sha256": "abc"},
		}}, nil
	}
	target := &fakeIndexAPI{}
	newCopyIndexAPI = func(requestor request.Requester) indexAPI { return target }

	err := Cmd.RunE(Cmd, []string{"local", "target", "Org/Proj"})
	if err != nil {
		t.Fatalf("RunE error: %v", err)
	}
	if runtimeCalls != 1 {
		t.Fatalf("expected only target runtime creation, got %d calls", runtimeCalls)
	}
	if len(target.createBulkReq) != 1 || len(target.createBulkReq[0].Records) != 1 {
		t.Fatalf("expected one created target record, got %+v", target.createBulkReq)
	}
}
