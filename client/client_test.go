package client

import "testing"

func TestNewValidatesConnectionOptions(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("expected missing endpoint error")
	}
	if _, err := New(Options{Endpoint: "https://example.test"}); err == nil {
		t.Fatal("expected missing project error")
	}
	if _, err := New(Options{Endpoint: "https://example.test", Project: "project"}); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestSafeRelativePath(t *testing.T) {
	for _, path := range []string{"/absolute", "../outside", "..", "."} {
		if _, err := safeRelativePath(path); err == nil {
			t.Fatalf("expected unsafe path %q to fail", path)
		}
	}
	if got, err := safeRelativePath("data/file.bin"); err != nil || got != "data/file.bin" {
		t.Fatalf("safe path = %q, err = %v", got, err)
	}
}
