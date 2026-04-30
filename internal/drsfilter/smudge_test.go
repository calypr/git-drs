package drsfilter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/calypr/git-drs/internal/common"
	"github.com/calypr/git-drs/internal/lfs"
)

func TestSmudgeContent_PassthroughNonPointer(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer

	err := SmudgeContent(context.Background(), "README.md", bytes.NewBufferString("plain-bytes\n"), &out, logger, nil)
	if err != nil {
		t.Fatalf("SmudgeContent returned error: %v", err)
	}

	if got := out.String(); got != "plain-bytes\n" {
		t.Fatalf("unexpected output: got %q", got)
	}
}

func TestSmudgeContent_UsesCacheWhenPresent(t *testing.T) {
	repo := setupSmudgeTestRepo(t)
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cachePath := mustObjectPath(t, oid)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("cached-content"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer
	downloaderCalled := false

	err := SmudgeContent(
		context.Background(),
		filepath.Join(repo, "data.txt"),
		bytes.NewBufferString(pointerForOID(oid, 14)),
		&out,
		logger,
		func(ctx context.Context, gotOID, gotPath string) error {
			downloaderCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("SmudgeContent returned error: %v", err)
	}
	if downloaderCalled {
		t.Fatal("expected downloader not to be called on cache hit")
	}
	if got := out.String(); got != "cached-content" {
		t.Fatalf("unexpected output: got %q", got)
	}
}

func TestSmudgeContent_DownloadsWhenCacheMiss(t *testing.T) {
	oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	setupSmudgeTestRepo(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer
	called := 0

	err := SmudgeContent(
		context.Background(),
		"file.bin",
		bytes.NewBufferString(pointerForOID(oid, 15)),
		&out,
		logger,
		func(ctx context.Context, gotOID, cachePath string) error {
			called++
			if gotOID != oid {
				return errors.New("unexpected oid")
			}
			if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(cachePath, []byte("downloaded-bytes"), 0o644)
		},
	)
	if err != nil {
		t.Fatalf("SmudgeContent returned error: %v", err)
	}
	if called != 1 {
		t.Fatalf("expected downloader to be called once, got %d", called)
	}
	if got := out.String(); got != "downloaded-bytes" {
		t.Fatalf("unexpected output: got %q", got)
	}
}

func TestSmudgeContent_WritesPointerWithoutDownloaderOnCacheMiss(t *testing.T) {
	oid := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	setupSmudgeTestRepo(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer

	err := SmudgeContent(
		context.Background(),
		"missing.bin",
		bytes.NewBufferString(pointerForOID(oid, 10)),
		&out,
		logger,
		nil,
	)
	if err != nil {
		t.Fatalf("SmudgeContent returned error: %v", err)
	}
	if got := out.String(); got != pointerForOID(oid, 10) {
		t.Fatalf("expected pointer passthrough, got %q", got)
	}
}

func setupSmudgeTestRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	return repo
}

func mustObjectPath(t *testing.T, oid string) string {
	t.Helper()
	path, err := lfs.ObjectPath(common.LFS_OBJS_PATH, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	return path
}

func pointerForOID(oid string, size int64) string {
	return fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, size)
}
