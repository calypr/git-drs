package prepush

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/lfs"
	"github.com/calypr/git-drs/internal/precommit_cache"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

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

func TestReadPushedRefsAndBranchesFromRefs(t *testing.T) {
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

			refs, err := readPushedRefs(tmp)
			if err != nil {
				t.Fatalf("readPushedRefs error: %v", err)
			}
			branches := branchesFromRefs(refs)

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

func TestLfsFilesFromCacheNormalizesOID(t *testing.T) {
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

	rawOID := strings.Repeat("a", 64)
	pathEntry := precommit_cache.PathEntry{
		Path:      "data/file.bin",
		LFSOID:    " sha256:" + rawOID + " ",
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
	if info.Oid != rawOID {
		t.Fatalf("expected normalized oid %q, got %q", rawOID, info.Oid)
	}
}

func TestBufferStdinCleansUpTempFileOnCopyError(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := ""
	_, err := bufferStdin(errReader{}, func(dir, pattern string) (*os.File, error) {
		f, createErr := os.CreateTemp(tmpDir, pattern)
		if createErr != nil {
			return nil, createErr
		}
		tmpPath = f.Name()
		return f, nil
	})
	if err == nil {
		t.Fatalf("expected bufferStdin error")
	}
	if _, statErr := os.Stat(tmpPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp file to be removed, stat err=%v", statErr)
	}
}

func TestSubmitPendingLFSMetaRequestWiring(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	gitCmd(t, repo, "config", "drs.remote.origin.token", "test-token")

	oid := strings.Repeat("b", 64)
	name := "obj-name"
	if err := drsobject.WriteObject(".git/drs/lfs/objects", &drsapi.DrsObject{
		Id:   "drs://local:obj-id",
		Name: ptrString(name),
		Size: 123,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}, oid); err != nil {
		t.Fatalf("write drs object: %v", err)
	}

	var gotPath, gotAuth, gotContentType, gotAccept string
	var gotBody metadataSubmitRequest
	restoreClient := stubPendingMetadataClient(t, func(req *http.Request) (*http.Response, error) {
		gotPath = req.URL.Path
		gotAuth = req.Header.Get("Authorization")
		gotContentType = req.Header.Get("Content-Type")
		gotAccept = req.Header.Get("Accept")
		defer req.Body.Close()
		if err := json.NewDecoder(req.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    req,
		}, nil
	})
	t.Cleanup(restoreClient)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := submitPendingLFSMeta(
		context.Background(),
		config.Remote("origin"),
		"https://example.test/  ",
		map[string]lfs.LfsFileInfo{"file.bin": {Oid: oid}},
		logger,
	)
	if err != nil {
		t.Fatalf("submitPendingLFSMeta: %v", err)
	}
	if gotPath != "/info/lfs/objects/metadata" {
		t.Fatalf("expected metadata endpoint path, got %q", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("expected auth header, got %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("expected content-type application/json, got %q", gotContentType)
	}
	if gotAccept != "application/vnd.git-lfs+json" {
		t.Fatalf("expected accept header application/vnd.git-lfs+json, got %q", gotAccept)
	}
	if len(gotBody.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(gotBody.Candidates))
	}
	if len(gotBody.Candidates[0].Checksums) == 0 {
		t.Fatalf("expected candidate checksums to be populated")
	}
}

func TestSubmitPendingLFSMetaStatusHandling(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	oid := strings.Repeat("c", 64)
	name := "obj-name"
	if err := drsobject.WriteObject(".git/drs/lfs/objects", &drsapi.DrsObject{
		Id:   "drs://local:obj-id",
		Name: ptrString(name),
		Size: 123,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}, oid); err != nil {
		t.Fatalf("write drs object: %v", err)
	}

	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		wantErr     bool
	}{
		{name: "ok", status: http.StatusOK, contentType: "application/json", body: "{}", wantErr: false},
		{name: "degrade 404", status: http.StatusNotFound, contentType: "application/json", body: "{}", wantErr: false},
		{name: "degrade 405", status: http.StatusMethodNotAllowed, contentType: "application/json", body: "{}", wantErr: false},
		{name: "degrade 501", status: http.StatusNotImplemented, contentType: "application/json", body: "{}", wantErr: false},
		{name: "degrade html", status: http.StatusInternalServerError, contentType: "text/html; charset=utf-8", body: "<html>error</html>", wantErr: false},
		{name: "hard fail 401", status: http.StatusUnauthorized, contentType: "application/json", body: "{\"error\":\"unauthorized\"}", wantErr: true},
		{name: "hard fail 500", status: http.StatusInternalServerError, contentType: "application/json", body: "{\"error\":\"server\"}", wantErr: true},
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreClient := stubPendingMetadataClient(t, func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: tc.status,
					Header:     http.Header{"Content-Type": []string{tc.contentType}},
					Body:       io.NopCloser(strings.NewReader(tc.body)),
					Request:    req,
				}, nil
			})
			t.Cleanup(restoreClient)

			err := submitPendingLFSMeta(
				context.Background(),
				config.Remote("origin"),
				"https://example.test",
				map[string]lfs.LfsFileInfo{"file.bin": {Oid: oid}},
				logger,
			)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
		})
	}
}

func TestResolveRemoteAuthHeaderBasicAuth(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	gitCmd(t, repo, "config", "drs.remote.origin.username", "alice")
	gitCmd(t, repo, "config", "drs.remote.origin.password", "secret")

	header, ok := resolveRemoteAuthHeader("origin")
	if !ok {
		t.Fatalf("expected auth header")
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	if header != want {
		t.Fatalf("expected %q, got %q", want, header)
	}
}

func TestResolveRemoteAuthHeaderPrefersBearerTokenOverBasicAuth(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	gitCmd(t, repo, "config", "drs.remote.origin.token", "test-token")
	gitCmd(t, repo, "config", "drs.remote.origin.username", "alice")
	gitCmd(t, repo, "config", "drs.remote.origin.password", "secret")

	header, ok := resolveRemoteAuthHeader("origin")
	if !ok {
		t.Fatalf("expected auth header")
	}
	if header != "Bearer test-token" {
		t.Fatalf("expected bearer token to win, got %q", header)
	}
}

func TestResolveRemoteAuthHeaderBasicAuthRequiresBothFields(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	gitCmd(t, repo, "config", "drs.remote.origin.username", "alice")

	header, ok := resolveRemoteAuthHeader("origin")
	if ok {
		t.Fatalf("expected no auth header, got %q", header)
	}
	if header != "" {
		t.Fatalf("expected empty header, got %q", header)
	}
}

func TestSubmitPendingLFSMetaRequestWiringBasicAuth(t *testing.T) {
	repo := setupGitRepo(t)
	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	gitCmd(t, repo, "config", "drs.remote.origin.username", "alice")
	gitCmd(t, repo, "config", "drs.remote.origin.password", "secret")

	oid := strings.Repeat("d", 64)
	name := "obj-name"
	if err := drsobject.WriteObject(".git/drs/lfs/objects", &drsapi.DrsObject{
		Id:   "drs://local:obj-id",
		Name: ptrString(name),
		Size: 123,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}, oid); err != nil {
		t.Fatalf("write drs object: %v", err)
	}

	var gotAuth string
	restoreClient := stubPendingMetadataClient(t, func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("{}")),
			Request:    req,
		}, nil
	})
	t.Cleanup(restoreClient)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	err := submitPendingLFSMeta(
		context.Background(),
		config.Remote("origin"),
		"https://example.test",
		map[string]lfs.LfsFileInfo{"file.bin": {Oid: oid}},
		logger,
	)
	if err != nil {
		t.Fatalf("submitPendingLFSMeta: %v", err)
	}

	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:secret"))
	if gotAuth != want {
		t.Fatalf("expected basic auth header %q, got %q", want, gotAuth)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
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

func stubPendingMetadataClient(t *testing.T, respond func(*http.Request) (*http.Response, error)) func() {
	t.Helper()
	orig := pendingMetadataClientFactory
	pendingMetadataClientFactory = func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(respond)}
	}
	return func() {
		pendingMetadataClientFactory = orig
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func ptrString(s string) *string { return &s }
