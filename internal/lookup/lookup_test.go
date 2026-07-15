package lookup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
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

func TestMissingSHA256ForScopeUsesProjectScopedEndpoint(t *testing.T) {
	t.Parallel()
	var gotRequest struct {
		Organization string   `json:"organization"`
		Project      string   `json:"project"`
		SHA256       []string `json:"sha256"`
	}
	httpClient := &http.Client{Transport: lookupRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != bulkMissingSHA256Path {
			return nil, fmt.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"checked":2,"missing_sha256":["missing"]}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})}
	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	client := raw.(*syclient.Client)
	ctx := &remoteruntime.GitContext{Client: client, Organization: "org", ProjectId: "project"}

	missing, err := MissingSHA256ForScope(context.Background(), ctx, []string{"present", "missing"})
	if err != nil {
		t.Fatalf("MissingSHA256ForScope returned error: %v", err)
	}
	if !slices.Equal(missing, []string{"missing"}) {
		t.Fatalf("unexpected missing values: %v", missing)
	}
	if gotRequest.Organization != "org" || gotRequest.Project != "project" || !slices.Equal(gotRequest.SHA256, []string{"present", "missing"}) {
		t.Fatalf("unexpected request payload: %+v", gotRequest)
	}
}

func TestMissingSHA256ForScopeReportsUnsupportedServer(t *testing.T) {
	t.Parallel()
	httpClient := &http.Client{Transport: lookupRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatalf("syclient.New: %v", err)
	}
	_, err = MissingSHA256ForScope(context.Background(), &remoteruntime.GitContext{Client: raw.(*syclient.Client), Organization: "org", ProjectId: "project"}, []string{"oid"})
	if !errors.Is(err, ErrBulkMissingSHA256Unsupported) {
		t.Fatalf("expected unsupported endpoint error, got %v", err)
	}
}
