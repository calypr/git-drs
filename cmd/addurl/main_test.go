package addurl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/calypr/git-drs/cmd/addurl/lfss3"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/spf13/cobra"
)

func TestRunAddURL_WritesPointerAndLFSObject(t *testing.T) {
	content := "hello world"
	sum := sha256.Sum256([]byte(content))
	shaHex := fmt.Sprintf("%x", sum[:])

	tempDir := t.TempDir()
	lfsRoot := filepath.Join(tempDir, "lfs")

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
	}

	resetStubs := stubAddURLDeps(t,
		func(ctx context.Context, in lfss3.InspectInput) (*lfss3.InspectResult, error) {
			return &lfss3.InspectResult{
				GitCommonDir: tempDir,
				LFSRoot:      lfsRoot,
				Bucket:       "bucket",
				Key:          "path/to/file.bin",
				WorktreeName: "file.bin",
				SizeBytes:    int64(len(content)),
				MetaSHA256:   "",
				ETag:         "abcd1234",
				LastModTime:  time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			}, nil
		},
		func(path string) (bool, error) {
			return true, nil
		},
		func(ctx context.Context, in lfss3.InspectInput) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(content)), nil
		},
	)
	t.Cleanup(resetStubs)

	cmd := NewCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	requireFlags(t, cmd)

	oldwd := mustChdir(t, tempDir)
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	if err := runAddURL(cmd, []string{"s3://bucket/path/to/file.bin"}); err != nil {
		t.Fatalf("runAddURL error: %v", err)
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
}

// deprecated test case: now that we always "trust" the client-provided SHA256, this case is not applicable
//func TestRunAddURL_SHA256Mismatch(t *testing.T) {
//	content := "checksum content"
//
//	resetStubs := stubAddURLDeps(t,
//		func(ctx context.Context, in lfss3.InspectInput) (*lfss3.InspectResult, error) {
//			return &lfss3.InspectResult{
//				GitCommonDir: t.TempDir(),
//				LFSRoot:      t.TempDir(),
//				Bucket:       "bucket",
//				Key:          "file.bin",
//				WorktreeName: "file.bin",
//				SizeBytes:    int64(len(content)),
//				MetaSHA256:   "",
//				ETag:         "abcd1234",
//				LastModTime:  time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
//			}, nil
//		},
//		func(path string) (bool, error) {
//			return false, nil
//		},
//		func(ctx context.Context, in lfss3.InspectInput) (io.ReadCloser, error) {
//			return io.NopCloser(strings.NewReader(content)), nil
//		},
//	)
//	t.Cleanup(resetStubs)
//
//	cmd := NewCommand()
//	requireFlags(t, cmd)
//	if err := cmd.Flags().Set("sha256", "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
//		t.Fatalf("set sha256 flag: %v", err)
//	}
//
//	err := runAddURL(cmd, []string{"s3://bucket/file.bin", "file.bin"})
//	if err == nil || !strings.Contains(err.Error(), "sha256Param mismatch") {
//		t.Fatalf("expected sha256 mismatch error, got %v", err)
//	}
//}

func stubAddURLDeps(
	t *testing.T,
	inspectFn func(context.Context, lfss3.InspectInput) (*lfss3.InspectResult, error),
	isTrackedFn func(string) (bool, error),
	agentFetchFn func(context.Context, lfss3.InspectInput) (io.ReadCloser, error),
) func() {
	t.Helper()
	origInspect := inspectS3ForLFS
	origIsTracked := isLFSTracked
	origFetch := agentFetchReader

	inspectS3ForLFS = inspectFn
	isLFSTracked = isTrackedFn
	agentFetchReader = agentFetchFn

	return func() {
		inspectS3ForLFS = origInspect
		isLFSTracked = origIsTracked
		agentFetchReader = origFetch
	}
}

func requireFlags(t *testing.T, cmd *cobra.Command) {
	t.Helper()
	if err := cmd.Flags().Set(s3_utils.AWS_KEY_FLAG_NAME, "key"); err != nil {
		t.Fatalf("set aws key: %v", err)
	}
	if err := cmd.Flags().Set(s3_utils.AWS_SECRET_FLAG_NAME, "secret"); err != nil {
		t.Fatalf("set aws secret: %v", err)
	}
	if err := cmd.Flags().Set(s3_utils.AWS_REGION_FLAG_NAME, "region"); err != nil {
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
