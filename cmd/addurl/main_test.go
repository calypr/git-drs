package addurl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/precommit_cache"
	"github.com/spf13/cobra"
)

func TestRunAddURL_WritesPointerAndLFSObject(t *testing.T) {
	content := "hello world"
	sum := sha256.Sum256([]byte(content))
	shaHex := fmt.Sprintf("%x", sum[:])

	tempDir := t.TempDir()
	lfsRoot := filepath.Join(tempDir, ".git", "lfs")

	// ensure a git repository exists so any git-based config lookups succeed
	cmdInit := exec.Command("git", "init")
	cmdInit.Dir = tempDir
	if out, err := cmdInit.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, out)
	}

	// create a minimal drs config so runAddURL doesn't fail with
	// "config file does not exist. Please run 'git drs init'..."
	configPaths := []string{
		filepath.Join(tempDir, ".git", "drs", "config.yaml"),
	}
	for _, p := range configPaths {
		// ensure parent dir exists for safety (e.g. .git should already exist from git init)
		if dir := filepath.Dir(p); dir != tempDir && dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		yamlConfig := `
default_remote: calypr-dev
remotes:
  calypr-dev:
    gen3:
      endpoint: https://calypr-dev.ohsu.edu
      project_id: cbds-monorepos
      bucket: cbds
`
		if err := os.WriteFile(p, []byte(yamlConfig), 0644); err != nil {
			t.Fatalf("write config %s: %v", p, err)
		}
		fmt.Fprintf(os.Stderr, "TestRunAddURL_WritesPointerAndLFSObject wrote mock config file %s\n", p)
	}

	service := NewAddURLService()
	resetStubs := stubAddURLDeps(t, service,
		func(ctx context.Context, in cloud.S3ObjectParameters) (*cloud.S3Object, error) {
			return &cloud.S3Object{
				Bucket:      "bucket",
				Key:         "path/to/file.bin",
				Path:        "file.bin",
				SizeBytes:   int64(len(content)),
				MetaSHA256:  "",
				ETag:        "abcd1234",
				LastModTime: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			}, nil
		},
		func(path string) (bool, error) {
			return true, nil
		},
		// download stub: write the LFS object into lfsRoot and return the sha
		func(ctx context.Context, info *cloud.S3Object, input cloud.S3ObjectParameters, lfsRootPath string) (string, string, error) {
			objPath := filepath.Join(lfsRootPath, "objects", shaHex[0:2], shaHex[2:4], shaHex)
			if err := os.MkdirAll(filepath.Dir(objPath), 0755); err != nil {
				return "", "", err
			}
			if err := os.WriteFile(objPath, []byte(content), 0644); err != nil {
				return "", "", err
			}
			return shaHex, objPath, nil
		},
	)
	t.Cleanup(resetStubs)

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	requireFlags(t, cmd)

	oldwd := mustChdir(t, tempDir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	if err := service.Run(cmd, []string{"s3://bucket/path/to/file.bin"}); err != nil {
		t.Fatalf("service.Run error: %v", err)
	}

	pointerPath := filepath.Join(tempDir, "path/to/file.bin")
	pointerBytes, err := os.ReadFile(pointerPath)
	if err != nil {
		t.Fatalf("read pointer file: %v", err)
	}
	expectedPointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		shaHex,
		len(content),
	)
	if string(pointerBytes) != expectedPointer {
		t.Fatalf("pointer mismatch: expected %q, got %q", expectedPointer, string(pointerBytes))
	}

	lfsObject := filepath.Join(lfsRoot, "objects", shaHex[0:2], shaHex[2:4], shaHex)
	if _, err := os.Stat(lfsObject); err != nil {
		t.Fatalf("expected LFS object at %s: %v", lfsObject, err)
	}

	drsObject, err := drsmap.DrsInfoFromOid(shaHex)
	if err != nil {
		t.Fatalf("read drs object: %v", err)
	}
	if len(drsObject.AccessMethods) == 0 {
		t.Fatalf("expected access methods in drs object")
	}
	if got := drsObject.AccessMethods[0].AccessURL.URL; got != "s3://bucket/path/to/file.bin" {
		t.Fatalf("unexpected access URL: %s", got)
	}
}

func TestUpdatePrecommitCacheWritesEntries(t *testing.T) {
	repo := setupGitRepo(t)
	path := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	oid := "sha256deadbeef"
	externalURL := "s3://bucket/data/file.bin"

	if err := updatePrecommitCache(context.Background(), logger, path, oid, externalURL); err != nil {
		t.Fatalf("updatePrecommitCache: %v", err)
	}

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	pathsDir := filepath.Join(cacheRoot, "paths")
	oidDir := filepath.Join(cacheRoot, "oids")

	pathEntryFile := filepath.Join(pathsDir, precommit_cache.EncodePath("data/file.bin")+".json")
	pathData, err := os.ReadFile(pathEntryFile)
	if err != nil {
		t.Fatalf("read path entry: %v", err)
	}
	var pathEntry precommit_cache.PathEntry
	if err := json.Unmarshal(pathData, &pathEntry); err != nil {
		t.Fatalf("unmarshal path entry: %v", err)
	}
	if pathEntry.Path != "data/file.bin" {
		t.Fatalf("expected path entry path to be %q, got %q", "data/file.bin", pathEntry.Path)
	}
	if pathEntry.LFSOID != oid {
		t.Fatalf("expected path entry oid to be %q, got %q", oid, pathEntry.LFSOID)
	}
	if pathEntry.UpdatedAt == "" {
		t.Fatalf("expected updated_at to be set")
	}

	oidSum := sha256.Sum256([]byte(oid))
	oidEntryFile := filepath.Join(oidDir, fmt.Sprintf("%x.json", oidSum[:]))
	oidData, err := os.ReadFile(oidEntryFile)
	if err != nil {
		t.Fatalf("read oid entry: %v", err)
	}
	var oidEntry precommit_cache.OIDEntry
	if err := json.Unmarshal(oidData, &oidEntry); err != nil {
		t.Fatalf("unmarshal oid entry: %v", err)
	}
	if oidEntry.LFSOID != oid {
		t.Fatalf("expected oid entry oid to be %q, got %q", oid, oidEntry.LFSOID)
	}
	if oidEntry.ExternalURL != externalURL {
		t.Fatalf("expected oid entry external_url to be %q, got %q", externalURL, oidEntry.ExternalURL)
	}
	if len(oidEntry.Paths) != 1 || oidEntry.Paths[0] != "data/file.bin" {
		t.Fatalf("expected oid entry paths to include data/file.bin, got %v", oidEntry.Paths)
	}
	if oidEntry.UpdatedAt == "" {
		t.Fatalf("expected oid entry updated_at to be set")
	}
}

func TestUpdatePrecommitCacheContentChanged(t *testing.T) {
	repo := setupGitRepo(t)
	path := filepath.Join(repo, "data", "file.bin")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	oldwd := mustChdir(t, repo)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	firstOID := "sha256first"
	secondOID := "sha256second"

	if err := updatePrecommitCache(context.Background(), logger, path, firstOID, "s3://bucket/first"); err != nil {
		t.Fatalf("updatePrecommitCache first: %v", err)
	}
	if err := updatePrecommitCache(context.Background(), logger, path, secondOID, "s3://bucket/second"); err != nil {
		t.Fatalf("updatePrecommitCache second: %v", err)
	}

	cacheRoot := filepath.Join(repo, ".git", "drs", "pre-commit", "v1")
	oidDir := filepath.Join(cacheRoot, "oids")

	firstSum := sha256.Sum256([]byte(firstOID))
	firstEntryFile := filepath.Join(oidDir, fmt.Sprintf("%x.json", firstSum[:]))
	firstData, err := os.ReadFile(firstEntryFile)
	if err != nil {
		t.Fatalf("read first oid entry: %v", err)
	}
	var firstEntry precommit_cache.OIDEntry
	if err := json.Unmarshal(firstData, &firstEntry); err != nil {
		t.Fatalf("unmarshal first oid entry: %v", err)
	}
	if len(firstEntry.Paths) != 0 {
		t.Fatalf("expected old oid entry paths to be empty, got %v", firstEntry.Paths)
	}

	secondSum := sha256.Sum256([]byte(secondOID))
	secondEntryFile := filepath.Join(oidDir, fmt.Sprintf("%x.json", secondSum[:]))
	secondData, err := os.ReadFile(secondEntryFile)
	if err != nil {
		t.Fatalf("read second oid entry: %v", err)
	}
	var secondEntry precommit_cache.OIDEntry
	if err := json.Unmarshal(secondData, &secondEntry); err != nil {
		t.Fatalf("unmarshal second oid entry: %v", err)
	}
	if !secondEntry.ContentChange {
		t.Fatalf("expected content_changed to be true")
	}
	if len(secondEntry.Paths) != 1 || secondEntry.Paths[0] != "data/file.bin" {
		t.Fatalf("expected new oid entry paths to include data/file.bin, got %v", secondEntry.Paths)
	}
}

// deprecated test case: now that we always "trust" the client-provided SHA256, this case is not applicable
//func TestRunAddURL_SHA256Mismatch(t *testing.T) {
//	...
//}

func stubAddURLDeps(
	t *testing.T,
	service *AddURLService,
	inspectFn func(context.Context, cloud.S3ObjectParameters) (*cloud.S3Object, error),
	isTrackedFn func(string) (bool, error),
	downloadFn func(context.Context, *cloud.S3Object, cloud.S3ObjectParameters, string) (string, string, error),
) func() {
	t.Helper()
	origInspect := service.inspectS3
	origIsTracked := service.isLFSTracked
	origDownload := service.download

	service.inspectS3 = inspectFn
	service.isLFSTracked = isTrackedFn
	service.download = downloadFn

	return func() {
		service.inspectS3 = origInspect
		service.isLFSTracked = origIsTracked
		service.download = origDownload
	}
}

func requireFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	if err := cmd.Flags().Set(cloud.AWS_KEY_FLAG_NAME, "key"); err != nil {
		t.Fatalf("set aws key: %v", err)
	}
	if err := cmd.Flags().Set(cloud.AWS_SECRET_FLAG_NAME, "secret"); err != nil {
		t.Fatalf("set aws secret: %v", err)
	}
	if err := cmd.Flags().Set(cloud.AWS_REGION_FLAG_NAME, "region"); err != nil {
		t.Fatalf("set aws region: %v", err)
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
func updatePrecommitCache(ctx context.Context, logger *slog.Logger, path, oid, externalURL string) error {
	cache, err := precommit_cache.Open(ctx)
	if err != nil {
		return err
	}
	return cache.UpdatePathEntry(ctx, logger, path, oid, externalURL)
}
