package gitrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBucketMapping_ProjectThenOrgFallback(t *testing.T) {
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

	if err := SetBucketMapping("CBDS", "", "org-bucket", "cbds"); err != nil {
		t.Fatalf("set org mapping: %v", err)
	}
	if err := SetBucketMapping("CBDS", "projA", "project-bucket", "cbds/proj-a"); err != nil {
		t.Fatalf("set project mapping: %v", err)
	}

	m, ok, err := GetBucketMapping("cbds", "proja")
	if err != nil {
		t.Fatalf("get project mapping: %v", err)
	}
	if !ok {
		t.Fatalf("expected project mapping to exist")
	}
	if m.Bucket != "project-bucket" || m.Prefix != "cbds/proj-a" {
		t.Fatalf("unexpected project mapping: %+v", m)
	}

	m2, ok, err := GetBucketMapping("cbds", "missing")
	if err != nil {
		t.Fatalf("get org fallback mapping: %v", err)
	}
	if !ok {
		t.Fatalf("expected org fallback mapping to exist")
	}
	if m2.Bucket != "org-bucket" || m2.Prefix != "cbds" {
		t.Fatalf("unexpected org fallback mapping: %+v", m2)
	}

	// sanity: still in repo; avoid flaky path resolution assertions in CI
	if _, err := filepath.EvalSymlinks(filepath.Join(tmpDir, ".git")); err != nil {
		t.Fatalf("expected git repo to exist: %v", err)
	}
}

func TestBucketMapping_ClearsStalePrefixOnOverwrite(t *testing.T) {
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

	if err := SetBucketMapping("CBDS", "projA", "project-bucket-v1", "cbds/proj-a"); err != nil {
		t.Fatalf("set project mapping with prefix: %v", err)
	}
	if err := SetBucketMapping("CBDS", "projA", "project-bucket-v2", ""); err != nil {
		t.Fatalf("overwrite project mapping without prefix: %v", err)
	}

	m, ok, err := GetBucketMapping("cbds", "proja")
	if err != nil {
		t.Fatalf("get project mapping: %v", err)
	}
	if !ok {
		t.Fatalf("expected project mapping to exist")
	}
	if m.Bucket != "project-bucket-v2" {
		t.Fatalf("unexpected project bucket: %+v", m)
	}
	if m.Prefix != "" {
		t.Fatalf("expected cleared project prefix, got %q", m.Prefix)
	}

	if err := SetBucketMapping("CBDS", "", "org-bucket-v1", "cbds/org"); err != nil {
		t.Fatalf("set org mapping with prefix: %v", err)
	}
	if err := SetBucketMapping("CBDS", "", "org-bucket-v2", ""); err != nil {
		t.Fatalf("overwrite org mapping without prefix: %v", err)
	}

	m2, ok, err := GetBucketMapping("cbds", "missing")
	if err != nil {
		t.Fatalf("get org mapping: %v", err)
	}
	if !ok {
		t.Fatalf("expected org mapping to exist")
	}
	if m2.Bucket != "org-bucket-v2" {
		t.Fatalf("unexpected org bucket: %+v", m2)
	}
	if m2.Prefix != "" {
		t.Fatalf("expected cleared org prefix, got %q", m2.Prefix)
	}
}
