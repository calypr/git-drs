package precommit_cache

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maypok86/otter"
)

func TestEncodeDecodePathRoundTrip(t *testing.T) {
	original := "data/nested file.txt"
	encoded := EncodePath(original)
	decoded, err := DecodePath(encoded)
	if err != nil {
		t.Fatalf("DecodePath error: %v", err)
	}
	if decoded != original {
		t.Fatalf("expected %q, got %q", original, decoded)
	}
}

func TestLookupExternalURLByOID(t *testing.T) {
	cache := newTestCache(t)
	oid := "sha256:abc123"
	entry := OIDEntry{
		LFSOID:      oid,
		Paths:       []string{"data/foo.bin"},
		ExternalURL: "s3://bucket/key",
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	writeJSON(t, cache.oidEntryFile(oid), entry)

	url, ok, err := cache.LookupExternalURLByOID(oid)
	if err != nil {
		t.Fatalf("LookupExternalURLByOID error: %v", err)
	}
	if !ok {
		t.Fatalf("expected external url to be present")
	}
	if url != entry.ExternalURL {
		t.Fatalf("expected %q, got %q", entry.ExternalURL, url)
	}
}

func TestResolveExternalURLByPath(t *testing.T) {
	cache := newTestCache(t)
	oid := "sha256:def456"
	now := time.Now().UTC().Format(time.RFC3339)
	writeJSON(t, cache.pathEntryFile("data/foo.bin"), PathEntry{
		Path:      "data/foo.bin",
		LFSOID:    oid,
		UpdatedAt: now,
	})
	writeJSON(t, cache.oidEntryFile(oid), OIDEntry{
		LFSOID:      oid,
		Paths:       []string{"data/foo.bin"},
		ExternalURL: "s3://bucket/other",
		UpdatedAt:   now,
	})

	url, ok, err := cache.ResolveExternalURLByPath("data/foo.bin")
	if err != nil {
		t.Fatalf("ResolveExternalURLByPath error: %v", err)
	}
	if !ok {
		t.Fatalf("expected to resolve external url")
	}
	if url != "s3://bucket/other" {
		t.Fatalf("expected url to match, got %q", url)
	}
}

func TestCheckExternalURLMismatch(t *testing.T) {
	if err := CheckExternalURLMismatch("s3://bucket/a", "s3://bucket/a"); err != nil {
		t.Fatalf("expected no mismatch, got %v", err)
	}
	if err := CheckExternalURLMismatch("", "s3://bucket/a"); err != nil {
		t.Fatalf("expected empty hint to skip mismatch, got %v", err)
	}
	if err := CheckExternalURLMismatch("s3://bucket/a", "s3://bucket/b"); err == nil {
		t.Fatalf("expected mismatch error")
	}
}

func TestStaleAfter(t *testing.T) {
	old := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	if !StaleAfter(old, time.Hour) {
		t.Fatalf("expected timestamp to be stale")
	}
	if StaleAfter("not-a-time", time.Hour) {
		t.Fatalf("expected invalid timestamp to be non-stale")
	}
}

func TestOpenCache(t *testing.T) {
	repo := setupGitRepo(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	cache, err := Open(context.Background())
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if cache.Root == "" || cache.PathsDir == "" || cache.OIDsDir == "" {
		t.Fatalf("expected cache paths to be set, got %+v", cache)
	}
}

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	root := t.TempDir()

	pc, err := otter.MustBuilder[string, *PathEntry](1000).Build()
	if err != nil {
		t.Fatalf("create path cache: %v", err)
	}
	oc, err := otter.MustBuilder[string, *OIDEntry](1000).Build()
	if err != nil {
		t.Fatalf("create oid cache: %v", err)
	}

	return &Cache{
		GitDir:    root,
		RepoRoot:  root, // Is this safe assumption for test? Yes if mock repo
		Root:      root,
		PathsDir:  filepath.Join(root, "paths"),
		OIDsDir:   filepath.Join(root, "oids"),
		StatePath: filepath.Join(root, "state.json"),
		PathCache: pc,
		OidCache:  oc,
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func setupGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test User")
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
}
