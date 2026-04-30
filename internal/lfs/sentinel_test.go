package lfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyntheticOIDFromETag(t *testing.T) {
	oid, err := SyntheticOIDFromETag("abcd1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(oid) != 64 {
		t.Fatalf("expected 64-char oid, got %q", oid)
	}
}

func TestWriteAndDetectAddURLSentinelObject(t *testing.T) {
	root := t.TempDir()
	oid, err := SyntheticOIDFromETag("etag-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	path, err := WriteAddURLSentinelObject(root, oid, "etag-abc", "s3://bucket/key")
	if err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected sentinel file: %v", err)
	}
	ok, err := IsAddURLSentinelObject(path)
	if err != nil {
		t.Fatalf("IsAddURLSentinelObject error: %v", err)
	}
	if !ok {
		t.Fatalf("expected sentinel detection true")
	}
}

func TestIsAddURLSentinelBytes(t *testing.T) {
	payload, err := buildAddURLSentinel("etag", "s3://bucket/key")
	if err != nil {
		t.Fatalf("build sentinel: %v", err)
	}
	if !IsAddURLSentinelBytes(payload) {
		t.Fatalf("expected sentinel bytes to be detected")
	}
	non := []byte("not-a-sentinel")
	if IsAddURLSentinelBytes(non) {
		t.Fatalf("did not expect non-sentinel to match")
	}
}

func TestWriteAddURLSentinelObjectCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path, err := WriteAddURLSentinelObject(root, oid, "etag", "s3://bucket/key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(root, "objects", oid[:2], oid[2:4], oid)
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}
