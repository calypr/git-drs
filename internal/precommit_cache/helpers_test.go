package precommit_cache

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncodePathRoundTripStability(t *testing.T) {
	original := "data/nested file.txt"
	encoded := EncodePath(original)
	if encoded == "" {
		t.Fatal("expected encoded path")
	}
	if strings.Contains(encoded, "/") {
		t.Fatalf("expected filesystem-safe encoding, got %q", encoded)
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

func TestReadOIDEntryAcceptsLegacyS3URL(t *testing.T) {
	root := t.TempDir()
	cache := &Cache{
		Root:     root,
		PathsDir: filepath.Join(root, "paths"),
		OIDsDir:  filepath.Join(root, "oids"),
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		t.Fatalf("mkdir oids: %v", err)
	}
	file := OIDEntryPath(cache, "sha256:deadbeef")
	raw := []byte(`{"lfs_oid":"sha256:deadbeef","paths":["data/file.bin"],"s3_url":"s3://bucket/key","updated_at":"2026-01-01T00:00:00Z","content_changed":false}`)
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatalf("write legacy oid entry: %v", err)
	}

	entry, err := ReadOIDEntry(cache, "sha256:deadbeef", "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("ReadOIDEntry error: %v", err)
	}
	if entry.ExternalURL != "s3://bucket/key" {
		t.Fatalf("expected legacy s3_url to map to ExternalURL, got %q", entry.ExternalURL)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	if strings.Contains(string(data), `"s3_url"`) {
		t.Fatalf("expected normalized marshal to omit legacy s3_url field: %s", string(data))
	}
	if !strings.Contains(string(data), `"external_url":"s3://bucket/key"`) {
		t.Fatalf("expected normalized marshal to write external_url: %s", string(data))
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
