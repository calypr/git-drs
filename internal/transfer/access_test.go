package transfer

import (
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/globusauth"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syclient "github.com/calypr/syfon/client"
)

func TestPlanAccessURLFallsBackAfterGlobusAccessResolution(t *testing.T) {
	globusID := "globus-access"
	httpsURL := &struct {
		Headers *[]string `json:"headers,omitempty"`
		Url     string    `json:"url"`
	}{Url: "https://public.example/object"}
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
	drsCtx := &remoteruntime.GitContext{Client: raw.(*syclient.Client), AccessMethodPolicy: "prefer:globus"}

	got, err := planAccessURL(t.Context(), drsCtx, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods})
	if err != nil || got.Url != httpsURL.Url {
		t.Fatalf("planned URL = %+v, %v; want HTTPS fallback", got, err)
	}
	drsCtx.AccessMethodPolicy = "require:globus"
	if _, err := planAccessURL(t.Context(), drsCtx, drsapi.DrsObject{Id: "object-1", AccessMethods: &methods}); err == nil || !strings.Contains(err.Error(), "destination_collection_unmapped") {
		t.Fatalf("required Globus error = %v", err)
	}
}

func TestAccessMethodPolicyModes(t *testing.T) {
	httpsID, globusID := "https-access", "globus-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeGlobus, AccessId: &globusID},
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
	}
	obj := drsapi.DrsObject{Id: "object-1", AccessMethods: &methods}
	t.Setenv(globusauth.TransferTokenEnv, "")
	t.Setenv(globusauth.TokenFileEnv, filepath.Join(t.TempDir(), "missing-tokens.json"))
	t.Setenv(globusDestCollectionEnv, "")

	if got, err := selectAccessMethodWithPolicy(obj, "auto"); err != nil || got.AccessId == nil || *got.AccessId != httpsID {
		t.Fatalf("auto selected %+v, %v; want HTTPS", got, err)
	}
	if got, err := selectAccessMethodWithPolicy(obj, "prefer:globus"); err != nil || got.AccessId == nil || *got.AccessId != httpsID {
		t.Fatalf("prefer selected %+v, %v; want HTTPS fallback", got, err)
	}
	if _, err := selectAccessMethodWithPolicy(obj, "require:globus"); err == nil || !strings.Contains(err.Error(), "globus=disabled") {
		t.Fatalf("require error = %v", err)
	}
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "destination")
	if got, err := selectAccessMethodWithPolicy(obj, "auto"); err != nil || got.AccessId == nil || *got.AccessId != httpsID {
		t.Fatalf("deterministic auto selected %+v, %v; want HTTPS", got, err)
	}
}

func TestAccessMethodReadinessHonorsUnavailable(t *testing.T) {
	available := false
	id := "https-access"
	state, reason := accessMethodReadiness(drsapi.AccessMethod{Type: drsapi.AccessMethodTypeHttps, AccessId: &id, Available: &available})
	if state != globusauth.Disabled || !strings.Contains(reason, "unavailable") {
		t.Fatalf("readiness = %s (%s)", state, reason)
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
	globusURL := func(source string) *struct {
		Headers *[]string `json:"headers,omitempty"`
		Url     string    `json:"url"`
	} {
		return &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "globus://" + source + "/object"}
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

func TestSelectAccessMethodHonorsGlobusPreference(t *testing.T) {
	httpsID := "https-access"
	globusID := "globus-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
		{Type: drsapi.AccessMethodTypeGlobus, AccessId: &globusID},
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "globus")
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "dest-collection")

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessId == nil || *method.AccessId != globusID {
		t.Fatalf("selected method = %+v, want Globus", method)
	}
}

func TestSelectAccessMethodSkipsUnconfiguredGlobus(t *testing.T) {
	httpsID := "https-access"
	globusID := "globus-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeGlobus, AccessId: &globusID},
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
	}
	t.Setenv(globusauth.TransferTokenEnv, "")
	t.Setenv(globusDestCollectionEnv, "")

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessId == nil || *method.AccessId != httpsID {
		t.Fatalf("selected method = %+v, want configured HTTPS", method)
	}
}

func TestSelectAccessMethodSkipsUnsupportedDirectURL(t *testing.T) {
	httpsID := "https-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeS3, AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "s3://bucket/object"}},
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
	}

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessId == nil || *method.AccessId != httpsID {
		t.Fatalf("selected method = %+v, want HTTPS", method)
	}
}

func TestSelectAccessMethodFallsBackToFirstResolvableMethod(t *testing.T) {
	httpsID := "https-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeGlobus},
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
	}
	t.Setenv("GIT_DRS_ACCESS_METHOD", "globus")

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessId == nil || *method.AccessId != httpsID {
		t.Fatalf("selected method = %+v, want HTTPS method with access ID", method)
	}
}

func TestSelectAccessMethodAcceptsDirectGlobusAccessURL(t *testing.T) {
	httpsID := "https-access"
	methods := []drsapi.AccessMethod{
		{Type: drsapi.AccessMethodTypeGlobus, AccessUrl: &struct {
			Headers *[]string `json:"headers,omitempty"`
			Url     string    `json:"url"`
		}{Url: "globus://source-collection/path/file.bam"}},
		{Type: drsapi.AccessMethodTypeHttps, AccessId: &httpsID},
	}
	t.Setenv(globusauth.TransferTokenEnv, "token")
	t.Setenv(globusDestCollectionEnv, "dest-collection")
	t.Setenv("GIT_DRS_ACCESS_METHOD", "prefer:globus")

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessUrl == nil || method.AccessUrl.Url != "globus://source-collection/path/file.bam" {
		t.Fatalf("selected method = %+v, want direct Globus access URL", method)
	}
}
