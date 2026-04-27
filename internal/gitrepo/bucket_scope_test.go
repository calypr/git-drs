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

	if err := SetBucketMapping("calypr", "", "mapped-bucket", "program-root"); err != nil {
		t.Fatalf("SetBucketMapping org: %v", err)
	}
	if err := SetBucketMapping("calypr", "proj1", "mapped-bucket", "proj1-root"); err != nil {
		t.Fatalf("SetBucketMapping project: %v", err)
	}

	got, err := ResolveBucketScope("calypr", "proj1", "", "")
	if err != nil {
		t.Fatalf("ResolveBucketScope error: %v", err)
	}
	if got.Bucket != "mapped-bucket" {
		t.Fatalf("bucket mismatch: %q", got.Bucket)
	}
	if got.Prefix != "program-root/proj1-root" {
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

	if err := SetBucketMapping("calypr", "", "mapped-bucket", ""); err != nil {
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

func TestResolveBucketScope_ProgramPathWithNoProjectSubpath(t *testing.T) {
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

	if err := SetBucketMapping("calypr", "", "program-bucket", "program-root"); err != nil {
		t.Fatalf("SetBucketMapping: %v", err)
	}

	got, err := ResolveBucketScope("calypr", "proj1", "", "")
	if err != nil {
		t.Fatalf("ResolveBucketScope error: %v", err)
	}
	if got.Bucket != "program-bucket" || got.Prefix != "program-root" {
		t.Fatalf("unexpected scope: %+v", got)
	}
}
