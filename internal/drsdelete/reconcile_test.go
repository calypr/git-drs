package drsdelete

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func TestReconcileCommittedDeletes_RemovesControlledAccess(t *testing.T) {
	repo := initRepoWithDelete(t, []pointerSpec{{Path: "data.dat", OID: strings.Repeat("a", 64)}})

	oldWD, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	oldSHA := gitRevParse(t, repo, "HEAD~1")
	newSHA := gitRevParse(t, repo, "HEAD")

	var removedResource string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/"+strings.Repeat("a", 64):
			obj := drsapi.DrsObject{
				Id:               "did-1",
				ControlledAccess: &[]string{"/organization/org/project/proj", "/organization/other/project/x"},
				Checksums:        []drsapi.Checksum{{Type: "sha256", Checksum: strings.Repeat("a", 64)}},
			}
			records := []drsapi.DrsObject{obj}
			writeJSON(t, w, http.StatusOK, drsapi.N200OkDrsObjects{ResolvedDrsObject: &records})
		case r.Method == http.MethodPost && r.URL.Path == "/index/did-1/controlled-access/remove":
			var req struct {
				Resource string `json:"resource"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode remove controlled access: %v", err)
			}
			removedResource = req.Resource
			writeJSON(t, w, http.StatusOK, map[string]any{"did": "did-1"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	drsCtx := newGitContext(t, server.URL)
	summary, err := ReconcileCommittedDeletes(context.Background(), drsCtx, []RefUpdate{{OldSHA: oldSHA, NewSHA: newSHA}}, nil)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if summary.RemovedResources != 1 {
		t.Fatalf("expected one removed resource, got %+v", summary)
	}
	if removedResource != "/organization/org/project/proj" {
		t.Fatalf("unexpected removed resource: %s", removedResource)
	}
}

func TestReconcileCommittedDeletes_DeletesWholeRecord(t *testing.T) {
	repo := initRepoWithDelete(t, []pointerSpec{{Path: "other.dat", OID: strings.Repeat("b", 64)}})

	oldWD, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	oldSHA := gitRevParse(t, repo, "HEAD~1")
	newSHA := gitRevParse(t, repo, "HEAD")

	deleted := false
	var deleteReq struct {
		DeleteObjectMetadata bool `json:"delete_object_metadata"`
		DeleteStorageData    bool `json:"delete_storage_data"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ga4gh/drs/v1/objects/checksum/"+strings.Repeat("b", 64):
			obj := drsapi.DrsObject{
				Id:               "did-2",
				ControlledAccess: &[]string{"/organization/org/project/proj"},
				Checksums:        []drsapi.Checksum{{Type: "sha256", Checksum: strings.Repeat("b", 64)}},
			}
			records := []drsapi.DrsObject{obj}
			writeJSON(t, w, http.StatusOK, drsapi.N200OkDrsObjects{ResolvedDrsObject: &records})
		case r.Method == http.MethodPut && r.URL.Path == "/ga4gh/drs/v1/objects/did-2/delete":
			if err := json.NewDecoder(r.Body).Decode(&deleteReq); err != nil {
				t.Fatalf("decode delete request: %v", err)
			}
			deleted = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	drsCtx := newGitContext(t, server.URL)
	summary, err := ReconcileCommittedDeletes(context.Background(), drsCtx, []RefUpdate{{OldSHA: oldSHA, NewSHA: newSHA}}, nil)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if summary.DeletedRecords != 1 || !deleted {
		t.Fatalf("expected full delete, deleted=%v summary=%+v", deleted, summary)
	}
	if !deleteReq.DeleteObjectMetadata || !deleteReq.DeleteStorageData {
		t.Fatalf("expected delete request to purge metadata and storage, got %+v", deleteReq)
	}
}

func TestReconcileCommittedDeletes_SkipsWhenOIDStillLive(t *testing.T) {
	oid := strings.Repeat("c", 64)
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "config", "filter.lfs.clean", "cat")
	runGit(t, repo, "config", "filter.lfs.smudge", "cat")
	runGit(t, repo, "config", "filter.lfs.process", "cat")
	runGit(t, repo, "config", "filter.lfs.required", "false")
	runGit(t, repo, "checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.dat filter=lfs diff=lfs merge=lfs -text\n"), 0o644); err != nil {
		t.Fatalf("write .gitattributes: %v", err)
	}
	writePointerFile(t, filepath.Join(repo, "data.dat"), oid, "12")
	writePointerFile(t, filepath.Join(repo, "copy.dat"), oid, "12")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add two pointers")
	runGit(t, repo, "rm", "--", "data.dat")
	runGit(t, repo, "commit", "-m", "delete one pointer")

	oldWD, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	oldSHA := gitRevParse(t, repo, "HEAD~1")
	newSHA := gitRevParse(t, repo, "HEAD")

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		t.Fatalf("unexpected remote mutation request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	drsCtx := newGitContext(t, server.URL)
	summary, err := ReconcileCommittedDeletes(context.Background(), drsCtx, []RefUpdate{{OldSHA: oldSHA, NewSHA: newSHA}}, nil)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}
	if called {
		t.Fatalf("expected no remote call when oid still live")
	}
	if summary.ClearedLocalOnly != 1 {
		t.Fatalf("expected local-only clear, got %+v", summary)
	}
}
