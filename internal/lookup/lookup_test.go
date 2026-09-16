package lookup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	internalapi "github.com/calypr/syfon/apigen/client/internalapi"
	syclient "github.com/calypr/syfon/client"
)

type lookupRoundTripFunc func(*http.Request) (*http.Response, error)

func (f lookupRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObjectsByHashesForScopeUsesOneBulkRequestAndFiltersByScope(t *testing.T) {
	t.Parallel()

	projectAccessID := "s3-project"
	orgAccessID := "s3-org"
	projectMethods := []drsapi.AccessMethod{{
		Type:     drsapi.AccessMethodTypeS3,
		AccessId: &projectAccessID,
	}}
	orgMethods := []drsapi.AccessMethod{{
		Type:     drsapi.AccessMethodTypeS3,
		AccessId: &orgAccessID,
	}}
	otherMethods := []drsapi.AccessMethod{{
		Type: drsapi.AccessMethodTypeS3,
	}}
	projectControlled := []string{"/organization/org1/project/proj1"}
	orgControlled := []string{"/organization/org1"}
	otherControlled := []string{"/organization/other/project/proj"}
	abcHashes := internalapi.HashInfo{"sha256": "abc"}
	defHashes := internalapi.HashInfo{"sha256": "def"}
	temporaryHashes := internalapi.HashInfo{"git-drs-placeholder": "temporary"}
	checksumResponse := struct {
		Results map[string][]internalapi.InternalRecord `json:"results"`
	}{Results: map[string][]internalapi.InternalRecord{
		"abc": {
			{Did: "obj-project", ControlledAccess: &projectControlled, Hashes: &abcHashes, AccessMethods: &projectMethods},
			{Did: "obj-org", ControlledAccess: &orgControlled, Hashes: &abcHashes, AccessMethods: &orgMethods},
		},
		"def": {
			{Did: "obj-other", ControlledAccess: &otherControlled, Hashes: &defHashes, AccessMethods: &otherMethods},
		},
		"temporary": {
			{Did: "obj-placeholder", ControlledAccess: &projectControlled, Hashes: &temporaryHashes, AccessMethods: &projectMethods},
		},
	}}
	checksumBody, err := json.Marshal(checksumResponse)
	if err != nil {
		t.Fatalf("marshal checksum response: %v", err)
	}

	requests := 0
	httpClient := &http.Client{Transport: lookupRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/index/bulk/hashes" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			return nil, io.EOF
		}
		var request internalapi.BulkHashesRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode bulk request: %v", err)
			return nil, err
		}
		if want := []string{"abc", "def", "temporary"}; !reflect.DeepEqual(request.Hashes, want) {
			t.Errorf("bulk hashes = %v, want %v", request.Hashes, want)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(string(checksumBody))),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)
	ctx := &remoteruntime.GitContext{Client: client, Organization: "org1", ProjectId: "proj1"}

	got, err := ObjectsByHashesForScope(context.Background(), ctx, []string{"sha256:abc", "sha256:def", "abc", "temporary"})
	if err != nil {
		t.Fatalf("ObjectsByHashesForScope returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one bulk request", requests)
	}
	if len(got["sha256:abc"]) != 2 {
		t.Fatalf("expected project and org-wide matches for abc, got %+v", got["sha256:abc"])
	}
	if len(got["sha256:def"]) != 0 {
		t.Fatalf("expected non-matching scope to be filtered, got %+v", got["sha256:def"])
	}
	if records := got["temporary"]; len(records) != 1 || records[0].Checksums[0].Type != "git-drs-placeholder" {
		t.Fatalf("expected placeholder checksum record, got %+v", records)
	}
}
