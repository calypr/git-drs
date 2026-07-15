package addref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/remoteruntime"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestCreateLfsPointer(t *testing.T) {
	obj := &drsapi.DrsObject{
		Size:      10,
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	}
	path := filepath.Join(t.TempDir(), "pointer")
	if err := lfs.CreateLfsPointer(obj, path); err != nil {
		t.Fatalf("CreateLfsPointer error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected pointer content")
	}
}

func TestCreateLfsPointer_NoChecksum(t *testing.T) {
	obj := &drsapi.DrsObject{}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing checksums")
	}
}

func TestCreateLfsPointer_NoSHA256(t *testing.T) {
	obj := &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "md5", Checksum: "md5"}}}
	if err := lfs.CreateLfsPointer(obj, filepath.Join(t.TempDir(), "pointer")); err == nil {
		t.Fatalf("expected error for missing sha256")
	}
}

func TestAddRefLocalOIDUsesDerivedSourceWhenSHA256Missing(t *testing.T) {
	obj := &drsapi.DrsObject{Checksums: []drsapi.Checksum{{Type: "md5", Checksum: "md5"}}}
	oid := addRefLocalOID("drs://example.org/object-1", "source", obj)
	if len(oid) != 64 {
		t.Fatalf("expected sha256-shaped derived oid, got %q", oid)
	}
	if oid != derivedSourceOID("drs://example.org/object-1", "source") {
		t.Fatalf("expected derived source oid, got %s", oid)
	}
}

func TestCreateDRSPointerPreservesSourceURI(t *testing.T) {
	obj := &drsapi.DrsObject{Size: 42}
	path := filepath.Join(t.TempDir(), "pointer")
	if err := lfs.CreateDRSPointer(obj, path, "drs://example.org/object-1"); err != nil {
		t.Fatalf("CreateDRSPointer error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	expected := "version https://calypr.github.io/spec/v1\noid drs://example.org/object-1\nsize 42\n"
	if string(data) != expected {
		t.Fatalf("pointer mismatch: expected %q, got %q", expected, string(data))
	}
}

func TestResolveAddRefObjectUsesSourceAuthorityRemoteCredentials(t *testing.T) {
	var primaryRequests int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests++
		http.Error(w, "primary remote must not resolve source DRS URI", http.StatusTeapot)
	}))
	defer primary.Close()

	var sourceRequests int
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests++
		if r.URL.Path != "/ga4gh/drs/v1/objects/object-1" {
			t.Fatalf("unexpected source path: %s", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "source-user" || pass != "source-pass" {
			t.Fatalf("expected source basic auth credentials, got ok=%v user=%q pass=%q", ok, user, pass)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"object-1","self_uri":"drs://` + r.Host + `/object-1","size":42}`))
	}))
	defer source.Close()

	cfg := &config.Config{
		DefaultRemote: "primary",
		Remotes: map[config.Remote]config.RemoteSelect{
			"primary": {Local: &config.LocalRemote{BaseURL: primary.URL}},
			"source":  {Local: &config.LocalRemote{BaseURL: source.URL, BasicUsername: "source-user", BasicPassword: "source-pass"}},
		},
	}
	primaryCtx, err := remoteruntime.New(cfg, "primary", drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("create primary runtime: %v", err)
	}

	obj, err := resolveAddRefObject(context.Background(), cfg, "primary", primaryCtx, "drs://"+strings.TrimPrefix(source.URL, "http://")+"/object-1")
	if err != nil {
		t.Fatalf("resolveAddRefObject: %v", err)
	}
	if obj.Id != "object-1" || obj.Size != 42 {
		t.Fatalf("unexpected object: %+v", obj)
	}
	if sourceRequests != 1 {
		t.Fatalf("expected one source request, got %d", sourceRequests)
	}
	if primaryRequests != 0 {
		t.Fatalf("expected primary not to be contacted, got %d requests", primaryRequests)
	}
}
