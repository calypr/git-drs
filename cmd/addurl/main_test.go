package addurl

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/drsmap"
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
