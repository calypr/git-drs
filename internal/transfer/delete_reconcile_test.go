package transfer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/remoteruntime"
	internalapi "github.com/calypr/syfon/apigen/client/internalapi"
	syclient "github.com/calypr/syfon/client"
)

type pointerSpec struct {
	Path string
	OID  string
}

func TestReconcileCommittedDeletesRemovesControlledAccess(t *testing.T) {
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
		case r.Method == http.MethodPost && r.URL.Path == "/index/bulk/hashes":
			oid := strings.Repeat("a", 64)
			hashes := internalapi.HashInfo{"sha256": oid}
			controlled := []string{"/organization/org/project/proj", "/organization/other/project/x"}
			record := internalapi.InternalRecord{Did: "did-1", ControlledAccess: &controlled, Hashes: &hashes}
			writeJSON(t, w, http.StatusOK, map[string]any{"results": map[string]any{oid: []internalapi.InternalRecord{record}}})
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

func TestReconcileCommittedDeletesDeletesWholeRecord(t *testing.T) {
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
		case r.Method == http.MethodPost && r.URL.Path == "/index/bulk/hashes":
			oid := strings.Repeat("b", 64)
			hashes := internalapi.HashInfo{"sha256": oid}
			controlled := []string{"/organization/org/project/proj"}
			record := internalapi.InternalRecord{Did: "did-2", ControlledAccess: &controlled, Hashes: &hashes}
			writeJSON(t, w, http.StatusOK, map[string]any{"results": map[string]any{oid: []internalapi.InternalRecord{record}}})
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

func TestReconcileCommittedDeletesSkipsWhenOIDStillLive(t *testing.T) {
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

func initRepoWithDelete(t *testing.T, specs []pointerSpec) string {
	t.Helper()
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
	for _, spec := range specs {
		writePointerFile(t, filepath.Join(repo, spec.Path), spec.OID, "12")
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-m", "add pointers")
	for _, spec := range specs {
		runGit(t, repo, "rm", "--", spec.Path)
	}
	runGit(t, repo, "commit", "-m", "delete pointers")
	return repo
}

func newGitContext(t *testing.T, serverURL string) *remoteruntime.GitContext {
	t.Helper()
	rawClient, err := syclient.New(serverURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client := rawClient.(*syclient.Client)
	return &remoteruntime.GitContext{
		Client:       client,
		Organization: "org",
		ProjectId:    "proj",
	}
}

func gitRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse %s failed: %v\n%s", ref, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode json: %v", err)
	}
}

func writePointerFile(t *testing.T, path, oid, size string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir pointer dir: %v", err)
	}
	content := "version https://git-lfs.github.com/spec/v1\n" +
		"oid sha256:" + oid + "\n" +
		"size " + size + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write pointer file: %v", err)
	}
}
