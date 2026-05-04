package drsmap

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/precommit_cache"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
	syfoncommon "github.com/calypr/syfon/common"
)

func setupTestRepo(t *testing.T) {
	t.Helper()
	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
}

func TestWriteObjectsForLFSFilesBackfillsMissingAuthzWithoutOverwritingURL(t *testing.T) {
	setupTestRepo(t)

	oid := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	explicitURL := "s3://bucket/existing/path"
	if err := drsobject.WriteObject(common.DRS_OBJS_PATH, &drsapi.DrsObject{
		Id: "did-1",
		AccessMethods: &[]drsapi.AccessMethod{{
			Type: drsapi.AccessMethodTypeS3,
			AccessUrl: &struct {
				Headers *[]string `json:"headers,omitempty"`
				Url     string    `json:"url"`
			}{Url: explicitURL},
		}},
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
	}, oid); err != nil {
		t.Fatalf("seed DRS object: %v", err)
	}

	builder := drsobject.NewBuilder("bucket", "proj")
	builder.Organization = "org"
	files := map[string]lfs.LfsFileInfo{
		oid: {Name: "file.txt", Size: 12, Oid: oid},
	}
	if err := WriteObjectsForLFSFiles(builder, files, WriteOptions{Logger: testLogger(t)}); err != nil {
		t.Fatalf("WriteObjectsForLFSFiles error: %v", err)
	}

	got, err := drsobject.ReadObject(common.DRS_OBJS_PATH, oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	method := (*got.AccessMethods)[0]
	if method.AccessUrl == nil || method.AccessUrl.Url != explicitURL {
		t.Fatalf("access url overwritten: %+v", method.AccessUrl)
	}
	authz := syfoncommon.AuthzMapFromAccessMethodAuthorizations(method.Authorizations)
	want := map[string][]string{"org": {"proj"}}
	if !equalAuthzMaps(authz, want) {
		t.Fatalf("unexpected authz: got=%v want=%v", authz, want)
	}
}

func TestWriteObjectsForLFSFilesPreservesExistingAuthz(t *testing.T) {
	setupTestRepo(t)

	oid := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	existingAuthz := syfoncommon.AccessMethodAuthorizationsFromAuthzMap(map[string][]string{"keep": {"me"}})
	if err := drsobject.WriteObject(common.DRS_OBJS_PATH, &drsapi.DrsObject{
		Id: "did-2",
		AccessMethods: &[]drsapi.AccessMethod{{
			Type:           drsapi.AccessMethodTypeS3,
			Authorizations: existingAuthz,
		}},
		Checksums: []drsapi.Checksum{{Type: "sha256", Checksum: oid}},
	}, oid); err != nil {
		t.Fatalf("seed DRS object: %v", err)
	}

	builder := drsobject.NewBuilder("bucket", "proj")
	builder.Organization = "org"
	files := map[string]lfs.LfsFileInfo{
		oid: {Name: "file.txt", Size: 12, Oid: oid},
	}
	if err := WriteObjectsForLFSFiles(builder, files, WriteOptions{Logger: testLogger(t)}); err != nil {
		t.Fatalf("WriteObjectsForLFSFiles error: %v", err)
	}

	got, err := drsobject.ReadObject(common.DRS_OBJS_PATH, oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	authz := syfoncommon.AuthzMapFromAccessMethodAuthorizations((*got.AccessMethods)[0].Authorizations)
	want := map[string][]string{"keep": {"me"}}
	if !equalAuthzMaps(authz, want) {
		t.Fatalf("existing authz overwritten: got=%v want=%v", authz, want)
	}
}

func TestWriteObjectsForLFSFilesPreferCacheURLPreservesAuthz(t *testing.T) {
	setupTestRepo(t)

	oid := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	builder := drsobject.NewBuilder("bucket", "proj")
	builder.Organization = "org"
	files := map[string]lfs.LfsFileInfo{
		oid: {Name: "file.txt", Size: 12, Oid: oid},
	}

	cache := makeTestCache(t, oid, "s3://cache/object")
	if err := WriteObjectsForLFSFiles(builder, files, WriteOptions{
		Cache:          cache,
		PreferCacheURL: true,
		Logger:         testLogger(t),
	}); err != nil {
		t.Fatalf("WriteObjectsForLFSFiles error: %v", err)
	}

	got, err := drsobject.ReadObject(common.DRS_OBJS_PATH, oid)
	if err != nil {
		t.Fatalf("ReadObject error: %v", err)
	}
	method := (*got.AccessMethods)[0]
	if method.AccessUrl == nil || method.AccessUrl.Url != "s3://cache/object" {
		t.Fatalf("expected cache URL, got %+v", method.AccessUrl)
	}
	authz := syfoncommon.AuthzMapFromAccessMethodAuthorizations(method.Authorizations)
	want := map[string][]string{"org": {"proj"}}
	if !equalAuthzMaps(authz, want) {
		t.Fatalf("unexpected authz after cache URL preference: got=%v want=%v", authz, want)
	}
}

func ptrString(s string) *string { return &s }

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func equalAuthzMaps(got, want map[string][]string) bool {
	if len(got) != len(want) {
		return false
	}
	for org, wantProjects := range want {
		gotProjects, ok := got[org]
		if !ok {
			return false
		}
		if len(gotProjects) != len(wantProjects) {
			return false
		}
		for i := range wantProjects {
			if gotProjects[i] != wantProjects[i] {
				return false
			}
		}
	}
	return true
}

func makeTestCache(t *testing.T, oid, externalURL string) *precommit_cache.Cache {
	t.Helper()
	root := t.TempDir()
	cache := &precommit_cache.Cache{
		Root:     root,
		PathsDir: filepath.Join(root, "paths"),
		OIDsDir:  filepath.Join(root, "oids"),
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		t.Fatalf("mkdir cache oid dir: %v", err)
	}
	entry := precommit_cache.OIDEntry{
		LFSOID:      oid,
		ExternalURL: externalURL,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal cache entry: %v", err)
	}
	sum := sha256.Sum256([]byte(oid))
	filename := filepath.Join(cache.OIDsDir, fmt.Sprintf("%x.json", sum[:]))
	if err := os.WriteFile(filename, body, 0o644); err != nil {
		t.Fatalf("write cache entry: %v", err)
	}
	return cache
}
