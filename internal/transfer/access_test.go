package transfer

import (
	"testing"

	"github.com/calypr/git-drs/internal/globusauth"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

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

	method := selectAccessMethod(drsapi.DrsObject{AccessMethods: &methods})
	if method == nil || method.AccessUrl == nil || method.AccessUrl.Url != "globus://source-collection/path/file.bam" {
		t.Fatalf("selected method = %+v, want direct Globus access URL", method)
	}
}
