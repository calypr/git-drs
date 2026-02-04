package prepush

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/calypr/git-drs/precommit_cache"
)

func TestPrepushCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "prepush")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}

func TestLfsFilesFromCache(t *testing.T) {
	repo := setupGitRepo(t)
	filePath := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("first"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "first")
	oldSHA := gitOutputString(t, repo, "rev-parse", "HEAD")

	if err := os.WriteFile(filePath, []byte("second"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "second")
	newSHA := gitOutputString(t, repo, "rev-parse", "HEAD")

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	cache := &precommit_cache.Cache{
		GitDir:    filepath.Join(repo, ".git"),
		Root:      cacheRoot,
		PathsDir:  filepath.Join(cacheRoot, "paths"),
		OIDsDir:   filepath.Join(cacheRoot, "oids"),
		StatePath: filepath.Join(cacheRoot, "state.json"),
	}
	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths dir: %v", err)
	}
	if err := os.MkdirAll(cache.OIDsDir, 0o755); err != nil {
		t.Fatalf("mkdir oids dir: %v", err)
	}
	pathEntry := precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    "oid-123",
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	pathEntryFile := filepath.Join(cache.PathsDir, precommit_cache.EncodePath(pathEntry.Path)+".json")
	writeJSON(t, pathEntryFile, pathEntry)

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refs := []pushedRef{{
		LocalRef:  "refs/heads/main",
		LocalSHA:  newSHA,
		RemoteRef: "refs/heads/main",
		RemoteSHA: oldSHA,
	}}

	lfsFiles, ok, err := lfsFilesFromCache(context.Background(), cache, refs, logger)
	if err != nil {
		t.Fatalf("lfsFilesFromCache: %v", err)
	}
	if !ok {
		t.Fatalf("expected cache to be usable")
	}
	info, exists := lfsFiles["data/file.bin"]
	if !exists {
		t.Fatalf("expected lfs info for data/file.bin")
	}
	if info.Oid != "oid-123" {
		t.Fatalf("expected oid to be oid-123, got %s", info.Oid)
	}
	if info.OidType != "sha256" {
		t.Fatalf("expected oid type sha256, got %s", info.OidType)
	}
	stat, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size != stat.Size() {
		t.Fatalf("expected size %d, got %d", stat.Size(), info.Size)
	}
}

func TestReadPushedBranches(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string // Sorted
	}{
		{
			name:     "single branch",
			input:    "refs/heads/main 1234 oid123 refs/heads/main 1234 oid456",
			expected: []string{"main"},
		},
		{
			name:     "multiple branches",
			input:    "refs/heads/main 123 oid refs/heads/main 456 oid\nrefs/heads/feature/foo 789 oid remote 000 oid",
			expected: []string{"feature/foo", "main"},
		},
		{
			name:     "ignore tags",
			input:    "refs/tags/v1.0 123 oid refs/tags/v1.0 123 oid",
			expected: []string{},
		},
		{
			name:     "empty input",
			input:    "",
			expected: []string{},
		},
		{
			name:     "malformed lines",
			input:    "just-garbage\nrefs/heads/ok 1 2 3",
			expected: []string{"ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp, err := os.CreateTemp("", "test-stdin")
			if err != nil {
				t.Fatalf("create temp: %v", err)
			}
			defer os.Remove(tmp.Name())

			if _, err := tmp.WriteString(tt.input); err != nil {
				t.Fatalf("write temp: %v", err)
			}

			// readPushedBranches seeks to 0 itself, but we pass the *os.File
			// which must be valid.
			branches, err := readPushedBranches(tmp)
			if err != nil {
				t.Fatalf("readPushedBranches error: %v", err)
			}

			if len(branches) != len(tt.expected) {
				t.Errorf("expected %d branches, got %d: %v", len(tt.expected), len(branches), branches)
				return
			}
			for i := range branches {
				if branches[i] != tt.expected[i] {
					t.Errorf("branch mismatch at %d: got %s, want %s", i, branches[i], tt.expected[i])
				}
			}

			tmp.Close()
		})
	}
}

func TestLfsFilesFromCacheStale(t *testing.T) {
	repo := setupGitRepo(t)
	filePath := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitCmd(t, repo, "add", "data/file.bin")
	gitCmd(t, repo, "commit", "-m", "first")
	sha := gitOutputString(t, repo, "rev-parse", "HEAD")

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	cache := &precommit_cache.Cache{
		GitDir:    filepath.Join(repo, ".git"),
		Root:      cacheRoot,
		PathsDir:  filepath.Join(cacheRoot, "paths"),
		OIDsDir:   filepath.Join(cacheRoot, "oids"),
		StatePath: filepath.Join(cacheRoot, "state.json"),
	}
	if err := os.MkdirAll(cache.PathsDir, 0o755); err != nil {
		t.Fatalf("mkdir paths dir: %v", err)
	}

	pathEntry := precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    "oid-123",
		UpdatedAt: time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339),
	}
	pathEntryFile := filepath.Join(cache.PathsDir, precommit_cache.EncodePath(pathEntry.Path)+".json")
	writeJSON(t, pathEntryFile, pathEntry)

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	refs := []pushedRef{{
		LocalRef:  "refs/heads/main",
		LocalSHA:  sha,
		RemoteRef: "refs/heads/main",
		RemoteSHA: "0000000000000000000000000000000000000000",
	}}

	_, ok, err := lfsFilesFromCache(context.Background(), cache, refs, logger)
	if err != nil {
		t.Fatalf("lfsFilesFromCache: %v", err)
	}
	if ok {
		t.Fatalf("expected cache to be stale")
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

func gitOutputString(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func mustChdir(t *testing.T, dir string) string {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	return old
}
