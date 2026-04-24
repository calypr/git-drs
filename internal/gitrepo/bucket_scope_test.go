package gitrepo

import (
	"os"
	"os/exec"
	"testing"
)

func TestResolveBucketScope_UsesMapping(t *testing.T) {
	tmpDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	if err := SetBucketMapping("calypr", "proj1", "mapped-bucket", "calypr/proj1"); err != nil {
		t.Fatalf("SetBucketMapping: %v", err)
	}

	got, err := ResolveBucketScope("calypr", "proj1", "", "")
	if err != nil {
		t.Fatalf("ResolveBucketScope error: %v", err)
	}
	if got.Bucket != "mapped-bucket" {
		t.Fatalf("bucket mismatch: %q", got.Bucket)
	}
	if got.Prefix != "calypr/proj1" {
		t.Fatalf("prefix mismatch: %q", got.Prefix)
	}
}

func TestResolveBucketScope_FallbackConfiguredBucket(t *testing.T) {
	tmpDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	got, err := ResolveBucketScope("calypr", "proj1", "configured-bucket", "fallback/prefix")
	if err != nil {
		t.Fatalf("ResolveBucketScope error: %v", err)
	}
	if got.Bucket != "configured-bucket" {
		t.Fatalf("bucket mismatch: %q", got.Bucket)
	}
	if got.Prefix != "fallback/prefix" {
		t.Fatalf("prefix mismatch: %q", got.Prefix)
	}
}

func TestResolveBucketScope_MappingOverridesConfiguredBucket(t *testing.T) {
	tmpDir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(orig)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if err := exec.Command("git", "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}

	if err := SetBucketMapping("calypr", "proj1", "mapped-bucket", ""); err != nil {
		t.Fatalf("SetBucketMapping: %v", err)
	}

	got, err := ResolveBucketScope("calypr", "proj1", "different-bucket", "")
	if err != nil {
		t.Fatalf("ResolveBucketScope error: %v", err)
	}
	if got.Bucket != "mapped-bucket" {
		t.Fatalf("expected mapped bucket, got %q", got.Bucket)
	}
}
