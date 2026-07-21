package addref

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/gitrepo"
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

func TestSafeDestinationRejectsSymlinkedParent(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	runGitCmd(t, repo, "init")
	if err := os.Symlink(outside, filepath.Join(repo, "data")); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if _, err := safeDestination(filepath.Join("data", "pointer")); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink destination to be rejected, got %v", err)
	}
}

func TestSafeDestinationRejectsSymlinkedFile(t *testing.T) {
	repo := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	runGitCmd(t, repo, "init")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "pointer")); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	if _, err := safeDestination("pointer"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink destination to be rejected, got %v", err)
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

func TestAddRefTrackingMakesDRSPointerDiscoverable(t *testing.T) {
	repo := t.TempDir()
	runGitCmd(t, repo, "init")
	runGitCmd(t, repo, "config", "user.email", "test@example.com")
	runGitCmd(t, repo, "config", "user.name", "Test User")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	const path = "population_descriptor.tsv"
	obj := &drsapi.DrsObject{Size: 200184}
	if err := lfs.CreateDRSPointer(obj, path, "drs://drs.anv0:v2_example"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitrepo.TrackReadOnly(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	runGitCmd(t, repo, "add", ".gitattributes", path)

	files, err := lfs.GetTrackedLfsFiles(drslog.NewNoOpLogger())
	if err != nil {
		t.Fatal(err)
	}
	info, ok := files[path]
	if !ok {
		t.Fatalf("expected add-ref pointer to be discoverable, got %+v", files)
	}
	if info.OidType != "drs" || info.Oid != "//drs.anv0:v2_example" || info.Size != 200184 {
		t.Fatalf("unexpected pointer inventory: %+v", info)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
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

func TestResolveAddRefObjectIgnoresPrimaryEvenWhenPrimaryCanResolve(t *testing.T) {
	var primaryRequests int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"primary-object","self_uri":"drs://primary/object-1","size":99}`))
	}))
	defer primary.Close()

	var sourceRequests int
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sourceRequests++
		if r.URL.Path != "/ga4gh/drs/v1/objects/object-1" {
			t.Fatalf("unexpected source path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"source-object","self_uri":"drs://` + r.Host + `/object-1","size":42}`))
	}))
	defer source.Close()

	cfg := &config.Config{
		DefaultRemote: "primary",
		Remotes: map[config.Remote]config.RemoteSelect{
			"primary": {Local: &config.LocalRemote{BaseURL: primary.URL}},
			"source":  {Local: &config.LocalRemote{BaseURL: source.URL}},
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
	if obj.Id != "source-object" || obj.Size != 42 {
		t.Fatalf("expected object from source DRS authority/resolver, got %+v", obj)
	}
	if sourceRequests != 1 {
		t.Fatalf("expected one source request, got %d", sourceRequests)
	}
	if primaryRequests != 0 {
		t.Fatalf("expected primary not to be contacted, got %d requests", primaryRequests)
	}
}

type fakeDRSObjectGetter struct {
	gotObjectID *string
	obj         drsapi.DrsObject
}

func (g fakeDRSObjectGetter) GetObject(_ context.Context, objectID string) (drsapi.DrsObject, error) {
	*g.gotObjectID = objectID
	return g.obj, nil
}

func TestResolveAddRefObjectUsesSourceAuthorityWhenNoRemoteMatches(t *testing.T) {
	var primaryRequests int
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryRequests++
		http.Error(w, "primary remote must not resolve source DRS URI", http.StatusTeapot)
	}))
	defer primary.Close()

	cfg := &config.Config{
		DefaultRemote: "primary",
		Remotes: map[config.Remote]config.RemoteSelect{
			"primary": {Local: &config.LocalRemote{BaseURL: primary.URL}},
		},
	}
	primaryCtx, err := remoteruntime.New(cfg, "primary", drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("create primary runtime: %v", err)
	}

	var gotEndpoint string
	var gotObjectID string
	oldGetter := newSourceDRSGetter
	newSourceDRSGetter = func(endpoint string) (drsObjectGetter, error) {
		gotEndpoint = endpoint
		return fakeDRSObjectGetter{
			gotObjectID: &gotObjectID,
			obj:         drsapi.DrsObject{Id: "source-object", Size: 42},
		}, nil
	}
	defer func() { newSourceDRSGetter = oldGetter }()

	obj, err := resolveAddRefObject(context.Background(), cfg, "primary", primaryCtx, "drs://source.example.org/object-1")
	if err != nil {
		t.Fatalf("resolveAddRefObject: %v", err)
	}
	if obj.Id != "source-object" || obj.Size != 42 {
		t.Fatalf("expected object from source DRS authority/resolver, got %+v", obj)
	}
	if gotEndpoint != "https://source.example.org" {
		t.Fatalf("expected source endpoint, got %q", gotEndpoint)
	}
	if gotObjectID != "object-1" {
		t.Fatalf("expected source object ID, got %q", gotObjectID)
	}
	if primaryRequests != 0 {
		t.Fatalf("expected primary not to be contacted, got %d requests", primaryRequests)
	}
}
