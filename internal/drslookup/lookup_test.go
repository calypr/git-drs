package drslookup

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
)

type lookupRoundTripFunc func(*http.Request) (*http.Response, error)

func (f lookupRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestObjectsByHashesForScopeFiltersByScope(t *testing.T) {
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
	checksumResponse := drsapi.N200OkDrsObjects{ResolvedDrsObject: &[]drsapi.DrsObject{
		{Id: "obj-project", ControlledAccess: &projectControlled, Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "abc"}}, AccessMethods: &projectMethods},
		{Id: "obj-org", ControlledAccess: &orgControlled, Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "abc"}}, AccessMethods: &orgMethods},
		{Id: "obj-other", ControlledAccess: &otherControlled, Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "def"}}, AccessMethods: &otherMethods},
	}}
	checksumBody, err := json.Marshal(checksumResponse)
	if err != nil {
		t.Fatalf("marshal checksum response: %v", err)
	}

	httpClient := &http.Client{Transport: lookupRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/abc":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(checksumBody))),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/def":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"resolved_drs_object":[]}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		default:
			return nil, io.EOF
		}
	})}

	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)
	ctx := &remoteruntime.GitContext{Client: client, Organization: "org1", ProjectId: "proj1"}

	got, err := ObjectsByHashesForScope(context.Background(), ctx, []string{"sha256:abc", "sha256:def"})
	if err != nil {
		t.Fatalf("ObjectsByHashesForScope returned error: %v", err)
	}
	if len(got["sha256:abc"]) != 2 {
		t.Fatalf("expected project and org-wide matches for abc, got %+v", got["sha256:abc"])
	}
	if len(got["sha256:def"]) != 0 {
		t.Fatalf("expected non-matching scope to be filtered, got %+v", got["sha256:def"])
	}
}
