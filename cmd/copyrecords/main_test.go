package copyrecords

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
	listResp          copyListRecordsResponse
	listFn            func(opts syservices.ListRecordsOptions) copyListRecordsResponse
	bulkDocsResp      []copyRecord
	bulkHashResp      copyBulkHashesResponse
	createBulkReq     []copyBulkCreateRequest
	createBulkErr     error
	overwriteBulkReq  []copyBulkOverwriteRequest
	overwriteBulkResp copyBulkOverwriteResponse
	overwriteBulkErr  error
}

func (f *fakeIndexAPI) List(ctx context.Context, opts syservices.ListRecordsOptions) (copyListRecordsResponse, error) {
	if f.listFn != nil {
		return f.listFn(opts), nil
	}
	return f.listResp, nil
}

func (f *fakeIndexAPI) BulkDocuments(ctx context.Context, dids []string) ([]copyRecord, error) {
	f.bulkDocsReq = append(f.bulkDocsReq, dids)
	return f.bulkDocsResp, nil
}

func (f *fakeIndexAPI) BulkHashes(ctx context.Context, hashes []string) (copyBulkHashesResponse, error) {
	return f.bulkHashResp, nil
}

func (f *fakeIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	f.createBulkReq = append(f.createBulkReq, req)
	if f.createBulkErr != nil {
		return copyListRecordsResponse{}, f.createBulkErr
	}
	return copyListRecordsResponse{Records: &req.Records}, nil
}

func (f *fakeIndexAPI) OverwriteBulk(ctx context.Context, req copyBulkOverwriteRequest) (copyBulkOverwriteResponse, error) {
	f.overwriteBulkReq = append(f.overwriteBulkReq, req)
	if f.overwriteBulkErr != nil {
		return copyBulkOverwriteResponse{}, f.overwriteBulkErr
	}
	if f.overwriteBulkResp.Processed == 0 {
		return copyBulkOverwriteResponse{Processed: len(req.Records), Created: len(req.Records)}, nil
	}
	return f.overwriteBulkResp, nil
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

func TestCopyProjectRecordsWithOptions_UsesBulkOverwrite(t *testing.T) {
	target := &fakeIndexAPI{overwriteBulkResp: copyBulkOverwriteResponse{
		Processed:       2,
		Created:         1,
		Replaced:        1,
		DIDMatched:      1,
		ChecksumMatched: 1,
	}}
	stats, err := copyProjectRecordsWithOptions(context.Background(), nil, []copyRecord{{Did: "did-1"}, {Did: "did-2"}}, target, "Org", "Project", 5000, false, true)
	if err != nil {
		t.Fatalf("copyProjectRecordsWithOptions returned error: %v", err)
	}
	if len(target.overwriteBulkReq) != 1 {
		t.Fatalf("expected one overwrite request, got %d", len(target.overwriteBulkReq))
	}
	req := target.overwriteBulkReq[0]
	if req.Organization != "Org" || req.Project != "Project" || len(req.Records) != 2 {
		t.Fatalf("unexpected overwrite request: %+v", req)
	}
	if stats.Created != 1 || stats.Updated != 1 || stats.Written != 2 || stats.Unchanged != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestCopyProjectRecords_UsesScopedSourceList(t *testing.T) {
	scopeCA := []string{"/organization/HTAN_INT/project/BForePC"}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			if opts.Organization == "HTAN_INT" && opts.ProjectID == "BForePC" {
				return copyListRecordsResponse{Records: &[]copyRecord{
					{Did: "did-in-scope", ControlledAccess: &scopeCA},
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

func TestCopyProjectRecords_PaginatesScopedSourceListOnly(t *testing.T) {
	scopeCA := []string{"/organization/HTAN_INT/project/BForePC"}
	seenOpts := []syservices.ListRecordsOptions{}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			seenOpts = append(seenOpts, opts)
			if opts.Organization == "HTAN_INT" && opts.ProjectID == "BForePC" {
				switch opts.Start {
				case "":
					return copyListRecordsResponse{Records: &[]copyRecord{
						{Did: "did-page-1", ControlledAccess: &scopeCA},
					}}
				case "did-page-1":
					return copyListRecordsResponse{Records: &[]copyRecord{
						{Did: "did-page-2", ControlledAccess: &scopeCA},
					}}
				}
			}
			return copyListRecordsResponse{Records: &[]copyRecord{}}
		},
	}
	target := &fakeIndexAPI{}

	records, err := listSourceRecordsByControlledAccess(context.Background(), source, "HTAN_INT", "BForePC", 1)
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
	if !found["did-page-1"] || !found["did-page-2"] {
		t.Fatalf("missing copied records: %+v", target.createBulkReq[0].Records)
	}
	for _, opts := range seenOpts {
		if opts.Organization != "HTAN_INT" || opts.ProjectID != "BForePC" {
			t.Fatalf("expected scoped list options, got %+v", opts)
		}
		if opts.Page != 0 {
			t.Fatalf("expected cursor pagination without page offsets, got %+v", opts)
		}
	}
}

func TestCopyProjectRecordsFromSourceIndex_WritesEachPageBeforeScanningNextPage(t *testing.T) {
	sourceHash := copyHashInfo{"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	sourceCA := []string{"/organization/HTAN_INT/project/BForePC"}
	listedPages := []int{}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			listedPages = append(listedPages, len(listedPages)+1)
			if opts.Start == "" {
				return copyListRecordsResponse{Records: &[]copyRecord{{
					Did:              "page-1-record",
					Hashes:           &sourceHash,
					ControlledAccess: &sourceCA,
				}}}
			}
			return copyListRecordsResponse{Records: &[]copyRecord{{
				Did:              "page-2-record",
				Hashes:           &sourceHash,
				ControlledAccess: &sourceCA,
			}}}
		},
	}
	targetErr := errors.New("target write failed")
	target := &fakeIndexAPI{createBulkErr: targetErr}

	_, err := copyProjectRecordsFromSourceIndex(context.Background(), nil, source, target, "HTAN_INT", "BForePC", 1, false)
	if err == nil {
		t.Fatal("expected target write error")
	}
	if !strings.Contains(err.Error(), "target write failed") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(target.createBulkReq) != 1 {
		t.Fatalf("expected one target write attempt, got %+v", target.createBulkReq)
	}
	if len(listedPages) != 1 || listedPages[0] != 1 {
		t.Fatalf("expected only page 1 to be scanned before target write failure, got pages %+v", listedPages)
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
	if got := err.Error(); !strings.Contains(got, "tracked oid bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb for path data/a.bin is missing local DRS metadata and no matching local payload was found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadLocalSourceRecords_ReconstructsMissingMetadataFromPayload(t *testing.T) {
	oldTracked := loadTrackedLfsFiles
	oldRead := readLocalDRSObject
	t.Cleanup(func() {
		loadTrackedLfsFiles = oldTracked
		readLocalDRSObject = oldRead
	})

	payload := []byte("local payload")
	oid := fmt.Sprintf("%x", sha256.Sum256(payload))
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	loadTrackedLfsFiles = func(_ *slog.Logger) (map[string]lfs.LfsFileInfo, error) {
		return map[string]lfs.LfsFileInfo{path: {Name: path, Oid: "sha256:" + oid, Size: int64(len(payload))}}, nil
	}
	readLocalDRSObject = func(string) (*drsapi.DrsObject, error) { return nil, errors.New("not found") }

	records, err := loadLocalSourceRecords("Org", "Proj")
	if err != nil {
		t.Fatalf("loadLocalSourceRecords error: %v", err)
	}
	if len(records) != 1 || records[0].Did == "" || records[0].Hashes == nil || (*records[0].Hashes)["sha256"] != oid || records[0].Name == nil || *records[0].Name != "data.bin" {
		t.Fatalf("unexpected reconstructed record: %+v", records)
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

func TestCmdRunE_UsesLocalTargetWithoutBuildingTargetRuntime(t *testing.T) {
	oldLoadCfg := loadCopyConfig
	oldRuntime := newCopyRuntime
	oldLocalTarget := newLocalTargetAPI
	oldIndexAPI := newCopyIndexAPI
	t.Cleanup(func() {
		loadCopyConfig = oldLoadCfg
		newCopyRuntime = oldRuntime
		newLocalTargetAPI = oldLocalTarget
		newCopyIndexAPI = oldIndexAPI
	})

	loadCopyConfig = func() (*config.Config, error) {
		return &config.Config{
			DefaultRemote: "dev",
			Remotes: map[config.Remote]config.RemoteSelect{
				"dev": {Local: &config.LocalRemote{BaseURL: "http://dev.example.test", Organization: "Org", ProjectID: "Proj", Bucket: "bucket"}},
			},
		}, nil
	}
	runtimeCalls := []config.Remote{}
	newCopyRuntime = func(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*remoteruntime.GitContext, error) {
		runtimeCalls = append(runtimeCalls, remote)
		return &remoteruntime.GitContext{
			Client: &syclient.Client{},
		}, nil
	}
	sourceCA := []string{"/organization/Org/project/Proj"}
	source := &fakeIndexAPI{
		listFn: func(opts syservices.ListRecordsOptions) copyListRecordsResponse {
			if opts.Page != 0 {
				t.Fatalf("expected cursor pagination without page offsets, got %+v", opts)
			}
			if opts.Start == "" {
				return copyListRecordsResponse{Records: &[]copyRecord{{
					Did:              "did-1",
					Hashes:           &copyHashInfo{"sha256": "abc"},
					ControlledAccess: &sourceCA,
				}}}
			}
			return copyListRecordsResponse{Records: &[]copyRecord{}}
		},
	}
	target := &fakeIndexAPI{}
	newCopyIndexAPI = func(requestor request.Requester) indexAPI { return source }
	newLocalTargetAPI = func() indexAPI { return target }

	err := Cmd.RunE(Cmd, []string{"dev", "local", "Org/Proj"})
	if err != nil {
		t.Fatalf("RunE error: %v", err)
	}
	if len(runtimeCalls) != 1 || runtimeCalls[0] != "dev" {
		t.Fatalf("expected only source runtime for dev, got %+v", runtimeCalls)
	}
	if len(target.createBulkReq) != 1 || len(target.createBulkReq[0].Records) != 1 {
		t.Fatalf("expected one local target write, got %+v", target.createBulkReq)
	}
}

func TestCmdRunE_RejectsLocalSourceAndTarget(t *testing.T) {
	oldLoadCfg := loadCopyConfig
	t.Cleanup(func() {
		loadCopyConfig = oldLoadCfg
	})
	loadCopyConfig = func() (*config.Config, error) {
		return &config.Config{}, nil
	}

	err := Cmd.RunE(Cmd, []string{"local", "local", "Org/Proj"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source and target cannot both be local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalSentinel_DoesNotShadowRemoteNamedLocal(t *testing.T) {
	cfg := &config.Config{Remotes: map[config.Remote]config.RemoteSelect{
		"local": {Local: &config.LocalRemote{BaseURL: "http://example.test"}},
	}}

	if isLocalSentinel(cfg, "local") {
		t.Fatal("configured remote named local must not be treated as repo-local")
	}
	if !isLocalSentinel(cfg, "@local") {
		t.Fatal("@local must always mean repo-local")
	}
}

func TestCmdRunE_MissingSourceRemoteListsConfiguredRemotes(t *testing.T) {
	oldLoadCfg := loadCopyConfig
	t.Cleanup(func() {
		loadCopyConfig = oldLoadCfg
	})
	loadCopyConfig = func() (*config.Config, error) {
		return &config.Config{
			DefaultRemote: "origin",
			Remotes: map[config.Remote]config.RemoteSelect{
				"origin": {Local: &config.LocalRemote{BaseURL: "http://origin.example.test"}},
				"prod":   {Local: &config.LocalRemote{BaseURL: "http://prod.example.test"}},
			},
		}, nil
	}

	err := Cmd.RunE(Cmd, []string{"dev", "local", "Org/Proj"})
	if err == nil {
		t.Fatal("expected error")
	}
	got := err.Error()
	if !strings.Contains(got, `source remote "dev" not found`) ||
		!strings.Contains(got, "Available remotes: origin, prod") ||
		!strings.Contains(got, "git drs remote list") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalIndexAPI_MergesByChecksumAndWritesLocalDRSObject(t *testing.T) {
	oldRead := readLocalDRSObject
	oldWrite := writeLocalDRSObject
	t.Cleanup(func() {
		readLocalDRSObject = oldRead
		writeLocalDRSObject = oldWrite
	})

	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	targetCA := []string{"/organization/Org/project/Existing"}
	sourceCA := []string{"/organization/Org/project/New"}
	targetMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/existing"},
	}}
	sourceMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeHttps,
		AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "https://example.test/copied"},
	}}
	readLocalDRSObject = func(gotOID string) (*drsapi.DrsObject, error) {
		if gotOID != oid {
			t.Fatalf("unexpected read oid %q", gotOID)
		}
		name := "existing.bin"
		return &drsapi.DrsObject{
			Id:               "did-existing",
			Name:             &name,
			Checksums:        []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
			ControlledAccess: &targetCA,
			AccessMethods:    &targetMethods,
			Size:             123,
		}, nil
	}
	var writtenOID string
	var writtenObj *drsapi.DrsObject
	writeLocalDRSObject = func(gotOID string, obj *drsapi.DrsObject) error {
		writtenOID = gotOID
		writtenObj = obj
		return nil
	}

	source := []copyRecord{{
		Did:              "did-source",
		Hashes:           &copyHashInfo{"sha256": oid},
		ControlledAccess: &sourceCA,
		AccessMethods:    &sourceMethods,
	}}
	stats, err := copyProjectRecords(context.Background(), nil, source, localIndexAPI{}, "Org", "New", 100, false)
	if err != nil {
		t.Fatalf("copyProjectRecords error: %v", err)
	}
	if stats.Updated != 1 || stats.Created != 0 || stats.Written != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	if writtenOID != oid {
		t.Fatalf("expected write oid %q, got %q", oid, writtenOID)
	}
	if writtenObj == nil {
		t.Fatal("expected written object")
	}
	if writtenObj.Id != "did-existing" {
		t.Fatalf("expected existing DID to be preserved, got %q", writtenObj.Id)
	}
	if writtenObj.ControlledAccess == nil || len(*writtenObj.ControlledAccess) != 2 {
		t.Fatalf("expected merged controlled access, got %+v", writtenObj.ControlledAccess)
	}
	if writtenObj.AccessMethods == nil || len(*writtenObj.AccessMethods) != 2 {
		t.Fatalf("expected merged access methods, got %+v", writtenObj.AccessMethods)
	}
}

func TestLocalIndexAPI_SkipsRecordsWithoutValidSHA256(t *testing.T) {
	oldWrite := writeLocalDRSObject
	t.Cleanup(func() {
		writeLocalDRSObject = oldWrite
	})

	validOID := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	written := []string{}
	writeLocalDRSObject = func(gotOID string, obj *drsapi.DrsObject) error {
		written = append(written, gotOID)
		return nil
	}

	resp, err := (localIndexAPI{}).CreateBulk(context.Background(), copyBulkCreateRequest{Records: []copyRecord{
		{
			Did:    "hashless-record",
			Hashes: &copyHashInfo{},
		},
		{
			Did:    "invalid-hash-record",
			Hashes: &copyHashInfo{"sha256": "not-a-valid-local-object-key"},
		},
		{
			Did:    "valid-record",
			Hashes: &copyHashInfo{"sha256": validOID},
		},
	}})
	if err != nil {
		t.Fatalf("CreateBulk error: %v", err)
	}
	if len(written) != 1 || written[0] != validOID {
		t.Fatalf("expected only valid oid to be written, got %+v", written)
	}
	if resp.Records == nil || len(*resp.Records) != 1 || (*resp.Records)[0].Did != "valid-record" {
		t.Fatalf("expected response to include only written records, got %+v", resp.Records)
	}
}
