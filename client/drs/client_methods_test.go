package drs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	datadrs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/hash"
)

func TestGetDownloadURLFromRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ga4gh/drs/v1/objects/") && strings.Contains(r.URL.Path, "/access/") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"url":"https://download.example/file"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl := newTestGitDrsClientWithEndpoint(t, srv.URL)
	cl.Config.Organization = "org"
	cl.Config.ProjectId = "proj"

	t.Run("no records", func(t *testing.T) {
		_, err := cl.getDownloadURLFromRecords(context.Background(), "oid1", nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing access methods", func(t *testing.T) {
		rec := datadrs.DRSObject{Id: "did-1"}
		_, err := cl.getDownloadURLFromRecords(context.Background(), "oid1", []datadrs.DRSObject{rec})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("blank access type", func(t *testing.T) {
		name := "f.bin"
		rec := datadrs.DRSObject{
			Id:   "did-1",
			Name: &name,
			AccessMethods: []datadrs.AccessMethod{
				{
					Type: "",
					Authorizations: &datadrs.Authorizations{
						BearerAuthIssuers: []string{"/programs/org/projects/proj"},
					},
				},
			},
		}
		_, err := cl.getDownloadURLFromRecords(context.Background(), "oid1", []datadrs.DRSObject{rec})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("success", func(t *testing.T) {
		name := "f.bin"
		rec := datadrs.DRSObject{
			Id:   "did-1",
			Name: &name,
			AccessMethods: []datadrs.AccessMethod{
				{
					Type: "s3",
					Authorizations: &datadrs.Authorizations{
						BearerAuthIssuers: []string{"/programs/org/projects/proj"},
					},
				},
			},
		}
		u, err := cl.getDownloadURLFromRecords(context.Background(), "oid1", []datadrs.DRSObject{rec})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u == nil || u.Url == "" {
			t.Fatalf("expected URL, got %#v", u)
		}
	})
}

func TestGetObjectByHashFiltersByProjectAuthz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/index") && r.URL.Query().Get("hash") != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
  "records": [
    {
      "did":"did-match",
      "file_name":"a.bin",
      "size":1,
      "urls":["s3://bucket/a.bin"],
      "authz":["/programs/org/projects/proj"],
      "hashes":{"sha256":"oid1"}
    },
    {
      "did":"did-other",
      "file_name":"b.bin",
      "size":1,
      "urls":["s3://bucket/b.bin"],
      "authz":["/programs/other/projects/proj"],
      "hashes":{"sha256":"oid1"}
    }
  ]
}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cl := newTestGitDrsClientWithEndpoint(t, srv.URL)
	cl.Config.Organization = "org"
	cl.Config.ProjectId = "proj"

	res, err := cl.GetObjectByHash(context.Background(), &hash.Checksum{Type: "sha256", Checksum: "oid1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 || res[0].Id != "did-match" {
		t.Fatalf("unexpected filtered records: %#v", res)
	}
}
