package copyrecords

import (
	"context"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syservices "github.com/calypr/syfon/client/services"
)

type fakeIndexAPI struct {
	listResp      copyListRecordsResponse
	listFn        func(opts syservices.ListRecordsOptions) copyListRecordsResponse
	bulkDocsResp  []copyRecord
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

func (f *fakeIndexAPI) CreateBulk(ctx context.Context, req copyBulkCreateRequest) (copyListRecordsResponse, error) {
	f.createBulkReq = append(f.createBulkReq, req)
	return copyListRecordsResponse{Records: &req.Records}, nil
}

func TestMergeExistingRecord_UnionsControlledAccessAndAccessMethodsOnly(t *testing.T) {
	dstName := "target.bin"
	srcName := "source.bin"
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
			FileName:         &dstName,
			Description:      &desc,
			ControlledAccess: &leftCA,
			AccessMethods:    &leftMethods,
		},
		copyRecord{
			Did:              "did-1",
			FileName:         &srcName,
			ControlledAccess: &rightCA,
			AccessMethods:    &rightMethods,
		},
		false,
	)

	if !changed {
		t.Fatalf("expected merge to report a change")
	}
	if merged.FileName == nil || *merged.FileName != dstName {
		t.Fatalf("expected target metadata to be preserved, got %+v", merged.FileName)
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

func TestMergeExistingRecord_OverwritesFileNameWhenFlagEnabled(t *testing.T) {
	dstName := "target.bin"
	dstDisplayName := "target-display"
	srcName := "nested/source.bin"
	srcDisplayName := "source-display"

	merged, changed := mergeExistingRecord(
		copyRecord{
			Did:      "did-1",
			FileName: &dstName,
			Name:     &dstDisplayName,
		},
		copyRecord{
			Did:      "did-1",
			FileName: &srcName,
			Name:     &srcDisplayName,
		},
		true,
	)

	if !changed {
		t.Fatalf("expected merge to report a change")
	}
	if merged.FileName == nil || *merged.FileName != srcName {
		t.Fatalf("expected source file_name to win, got %+v", merged.FileName)
	}
	if merged.Name == nil || *merged.Name != srcDisplayName {
		t.Fatalf("expected source name to win, got %+v", merged.Name)
	}
}

func TestBuildMergedBatch_CreatesNewAndUpdatesExisting(t *testing.T) {
	srcCA := []string{"/organization/A/project/P1"}
	newCA := []string{"/organization/A/project/P2"}
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
				ControlledAccess: &srcCA,
				AccessMethods:    &srcMethods,
			},
		},
	}

	source := []copyRecord{
		{
			Did:              "did-existing",
			ControlledAccess: &newCA,
			AccessMethods:    &newMethods,
		},
		{
			Did:              "did-new",
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

func TestCopyProjectRecords_FallsBackToRootListWhenScopedListIsEmpty(t *testing.T) {
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

	stats, err := copyProjectRecords(context.Background(), nil, source, target, "HTAN_INT", "BForePC", 100, false)
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
