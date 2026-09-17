package transfer

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/drs"
	syclient "github.com/calypr/syfon/client"
)

func TestPlanAccessURLFallsBackAfterGlobusAccessResolution(t *testing.T) {
	globusID := "globus-access"
	httpsURL := &drsapi.AccessURL{Url: "https://public.example/object"}
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeGlobus, AccessId: &globusID},
		{Type: drsapi.AccessMethodTypeHttps, AccessUrl: httpsURL},
	}
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"url":"globus://source-a/object"}`)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    r,
		}, nil
	})}
	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "")
	drsCtx := &remoteruntime.GitContext{Client: raw, AccessMethodPolicy: "prefer:globus"}

	got, err := planAccessURL(t.Context(), drsCtx, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods})
	if err != nil || got.Url != httpsURL.Url {
		t.Fatalf("planned URL = %+v, %v; want HTTPS fallback", got, err)
	}
	drsCtx.AccessMethodPolicy = "require:globus"
	if _, err := planAccessURL(t.Context(), drsCtx, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods}); err == nil || !strings.Contains(err.Error(), "destination_collection_unmapped") {
		t.Fatalf("required Globus error = %v", err)
	}
}

func TestPlanAccessURLRejectsHostlessHTTP(t *testing.T) {
	t.Run("resolve access ID", func(t *testing.T) {
		accessID := "signed"
		methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeHttps, AccessId: &accessID, AccessUrl: &drsapi.AccessURL{Url: "https:///object"}}}
		var requestPath string
		httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			requestPath = r.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"url":"https://signed.example/object"}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Request:    r,
			}, nil
		})}
		raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
		if err != nil {
			t.Fatal(err)
		}
		got, err := planAccessURL(t.Context(), &remoteruntime.GitContext{Client: raw}, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods})
		if err != nil || got.Url != "https://signed.example/object" || requestPath != "/ga4gh/drs/v1/objects/object-1/access/signed" {
			t.Fatalf("planned URL = %+v, path = %q, error = %v", got, requestPath, err)
		}
	})

	t.Run("try next method", func(t *testing.T) {
		methods := []drsapi.AccessMethod{
			{Type: drsapi.AccessMethodTypeHttps, AccessUrl: &drsapi.AccessURL{Url: "https:///object"}},
			{Type: drsapi.AccessMethodTypeHttps, AccessUrl: &drsapi.AccessURL{Url: "https://public.example/object"}},
		}
		got, err := planAccessURL(t.Context(), &remoteruntime.GitContext{}, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods})
		if err != nil || got.Url != "https://public.example/object" {
			t.Fatalf("planned URL = %+v, error = %v", got, err)
		}
	})
}

func TestResolvedAccessReadinessAllowsFilesystemOnlyForLocalRemote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object")
	local := &remoteruntime.GitContext{RemoteType: config.LocalServerType}
	if state, _ := resolvedAccessReadiness(local, path); state != globusauth.Ready {
		t.Fatalf("local filesystem readiness = %s, want ready", state)
	}
	remote := &remoteruntime.GitContext{RemoteType: config.Gen3ServerType}
	if state, _ := resolvedAccessReadiness(remote, path); state != globusauth.Disabled {
		t.Fatalf("remote filesystem readiness = %s, want disabled", state)
	}
}

func TestAccessPolicyPrecedence(t *testing.T) {
	ctx := &remoteruntime.GitContext{AccessMethodPolicy: "prefer:https", CommandAccessMethod: "globus"}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "prefer:s3")
	if got := accessPolicyFor(ctx); got != "require:globus" {
		t.Fatalf("command policy = %q", got)
	}
	ctx.CommandAccessMethod = ""
	if got := accessPolicyFor(ctx); got != "prefer:s3" {
		t.Fatalf("environment policy = %q", got)
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "")
	if got := accessPolicyFor(ctx); got != "prefer:https" {
		t.Fatalf("remote policy = %q", got)
	}
}

func TestBulkAccessRequestAggregatesSelectionDiagnostics(t *testing.T) {
	globusURL := func(source string) *drsapi.AccessURL {
		return &drsapi.AccessURL{Url: "globus://" + source + "/object"}
	}
	objects := []drsapi.DrsObject{
		{Id: "one", AccessMethods: &[]drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeGlobus, AccessUrl: globusURL("source-one")}}},
		{Id: "two", AccessMethods: &[]drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeGlobus, AccessUrl: globusURL("source-two")}}},
	}
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "")
	drsCtx := &remoteruntime.GitContext{Client: &syclient.Client{}, AccessMethodPolicy: "require:globus"}
	_, err := BulkAccessURLsForObjects(t.Context(), drsCtx, objects)
	if err == nil || !strings.Contains(err.Error(), "object one") || !strings.Contains(err.Error(), "object two") {
		t.Fatalf("error=%v", err)
	}
}

func TestBulkResolvedAccessSelectsPolicyOrderNotResponseOrder(t *testing.T) {
	globusID := "globus-id"
	httpsID := "https-id"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
		{Type: drsapi.AccessMethodTypeGlobus, AccessId: &globusID},
	}
	var bulkBody []byte
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/ga4gh/drs/v1/objects/access" {
			return nil, fmt.Errorf("unexpected request path %s", r.URL.Path)
		}
		bulkBody, _ = io.ReadAll(r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"resolved_drs_object_access_urls":[` +
				`{"drs_object_id":"obj-1","drs_access_id":"https-id","url":"https://fallback.example/object","headers":["X-Access: fallback"]},` +
				`{"drs_object_id":"obj-1","drs_access_id":"globus-id","url":"globus://source.example/object","headers":["X-Access: preferred"]}` +
				`]}`)),
			Header:  http.Header{"Content-Type": []string{"application/json"}},
			Request: r,
		}, nil
	})}
	raw, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "destination-collection")
	drsCtx := &remoteruntime.GitContext{Client: raw, AccessMethodPolicy: "prefer:globus"}
	got, err := BulkResolvedAccessURLsForObjects(t.Context(), drsCtx, []drsapi.DrsObject{{Id: "obj-1", AccessMethods: &methods}})
	if err != nil {
		t.Fatalf("BulkResolvedAccessURLsForObjects returned error: %v", err)
	}
	resolved, ok := got["obj-1"]
	if !ok || resolved.AccessID != globusID || resolved.AccessURL.Url != "globus://source.example/object" {
		t.Fatalf("resolved access = %+v, want preferred Globus result", resolved)
	}
	if resolved.AccessURL.Headers == nil || (*resolved.AccessURL.Headers)[0] != "X-Access: preferred" {
		t.Fatalf("resolved headers = %+v, want preferred headers", resolved.AccessURL.Headers)
	}
	if !strings.Contains(string(bulkBody), `"https-id"`) || !strings.Contains(string(bulkBody), `"globus-id"`) {
		t.Fatalf("bulk request omitted an ordered access ID: %s", bulkBody)
	}
}

func TestBulkResolvedAccessFallsBackOnDuplicateMethodResults(t *testing.T) {
	accessID := "https-id"
	methods := []drsapi.AccessMethod{{Type: drsapi.AccessMethodTypeHttps, AccessId: &accessID}}
	var individualRequests int
	httpClient := &http.Client{Transport: downloadRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/ga4gh/drs/v1/objects/access":
			body = `{"resolved_drs_object_access_urls":[` +
				`{"drs_object_id":"obj-1","drs_access_id":"https-id","url":"https://one.example/object"},` +
				`{"drs_object_id":"obj-1","drs_access_id":"https-id","url":"https://two.example/object"}` +
				`]}`
		case "/ga4gh/drs/v1/objects/obj-1/access/https-id":
			individualRequests++
			body = `{"url":"https://stable.example/object"}`
		default:
			return nil, fmt.Errorf("unexpected request path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r}, nil
	})}
	client, err := syclient.New("http://example.test", syclient.WithHTTPClient(httpClient))
	if err != nil {
		t.Fatal(err)
	}
	got, err := BulkResolvedAccessURLsForObjects(t.Context(), &remoteruntime.GitContext{Client: client}, []drsapi.DrsObject{{Id: "obj-1", AccessMethods: &methods}})
	if err != nil {
		t.Fatal(err)
	}
	if individualRequests != 1 || got["obj-1"].AccessURL.Url != "https://stable.example/object" {
		t.Fatalf("duplicate bulk result did not fall back deterministically: requests=%d result=%+v", individualRequests, got["obj-1"])
	}
}
