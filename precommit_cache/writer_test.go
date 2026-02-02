package precommit_cache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdatePathEntry(t *testing.T) {
	cache := newTestCache(t)
	// Use io.Discard directly for slog
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	path := "test.txt"
	oid := "sha256:1234567890abcdef"
	extURL := "https://example.com/file"

	// 1. New entry
	err := cache.UpdatePathEntry(ctx, logger, path, oid, extURL)
	if err != nil {
		t.Fatalf("UpdatePathEntry failed: %v", err)
	}

	// Verify PathEntry
	pe, ok, err := cache.ReadPathEntry(path)
	if err != nil || !ok {
		t.Fatalf("PathEntry not found: ok=%v, err=%v", ok, err)
	}
	if pe.LFSOID != oid {
		t.Errorf("expected OID %s, got %s", oid, pe.LFSOID)
	}

	// Verify OIDEntry
	oe, ok, err := cache.ReadOIDEntry(oid)
	if err != nil || !ok {
		t.Fatalf("OIDEntry not found: ok=%v, err=%v", ok, err)
	}
	found := false
	for _, p := range oe.Paths {
		if p == path {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected path %s in OIDEntry paths, got %v", path, oe.Paths)
	}
	if oe.ExternalURL != extURL {
		t.Errorf("expected URL %s, got %s", extURL, oe.ExternalURL)
	}

	// 2. Update entry with same OID (should update URL)
	newURL := "https://example.com/new"
	err = cache.UpdatePathEntry(ctx, logger, path, oid, newURL)
	if err != nil {
		t.Fatalf("UpdatePathEntry repeat failed: %v", err)
	}
	oe, _, _ = cache.ReadOIDEntry(oid)
	if oe.ExternalURL != newURL {
		t.Errorf("expected updated URL %s, got %s", newURL, oe.ExternalURL)
	}

	// 3. Update entry with different OID
	newOID := "sha256:fedcba9876543210"
	err = cache.UpdatePathEntry(ctx, logger, path, newOID, "")
	if err != nil {
		t.Fatalf("UpdatePathEntry new OID failed: %v", err)
	}

	// Verify old OIDEntry no longer has the path
	oeOld, _, _ := cache.ReadOIDEntry(oid)
	for _, p := range oeOld.Paths {
		if p == path {
			t.Errorf("path %s still exists in old OIDEntry", path)
		}
	}

	// Verify new OIDEntry has the path and ContentChange is true
	oeNew, _, _ := cache.ReadOIDEntry(newOID)
	found = false
	for _, p := range oeNew.Paths {
		if p == path {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("path %s missing from new OIDEntry", path)
	}
	if !oeNew.ContentChange {
		t.Errorf("expected ContentChange to be true")
	}
}

func TestDeletePathEntry(t *testing.T) {
	cache := newTestCache(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	path := "delete-me.txt"
	oid := "sha256:999"
	_ = cache.UpdatePathEntry(ctx, logger, path, oid, "")

	err := cache.DeletePathEntry(ctx, logger, path)
	if err != nil {
		t.Fatalf("DeletePathEntry failed: %v", err)
	}

	// Verify vanished
	_, ok, _ := cache.ReadPathEntry(path)
	if ok {
		t.Errorf("PathEntry still exists in cache")
	}

	oe, _, _ := cache.ReadOIDEntry(oid)
	for _, p := range oe.Paths {
		if p == path {
			t.Errorf("path still exists in OIDEntry")
		}
	}
}

func TestRelPath(t *testing.T) {
	// Need a real temp dir for filepath.EvalSymlinks to work reliably
	tempDir := t.TempDir()
	root, _ := filepath.EvalSymlinks(tempDir)
	cache := &Cache{RepoRoot: root}

	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"file.txt", "file.txt", false},
		{"./file.txt", "file.txt", false},
		{"sub/dir/file.txt", "sub/dir/file.txt", false},
		{filepath.Join(root, "abs-file.txt"), "abs-file.txt", false},
		{"../outside.txt", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			// If it's an absolute path expected to be in root, create it
			if filepath.IsAbs(tt.input) && !tt.wantErr {
				_ = os.MkdirAll(filepath.Dir(tt.input), 0o755)
				_ = os.WriteFile(tt.input, []byte(""), 0o644)
			}

			got, err := cache.relPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("relPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("relPath(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
