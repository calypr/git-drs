package drsdelete

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/config"
	syclient "github.com/calypr/syfon/client"
)

type pointerSpec struct {
	Path string
	OID  string
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

func newGitContext(t *testing.T, serverURL string) *config.GitContext {
	t.Helper()
	rawClient, err := syclient.New(serverURL)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client := rawClient.(*syclient.Client)
	return &config.GitContext{
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
