package filter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
)

func TestSmudgeContentPassthroughNonPointer(t *testing.T) {
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

func TestSmudgeContentStreamsLargeNonPointer(t *testing.T) {
	const payloadSize = 32 << 20
	got := &countingHashWriter{hash: sha256.New()}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := SmudgeContent(context.Background(), "large.bin", &repeatingReader{remaining: payloadSize, value: 'x'}, got, nil, nil)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("SmudgeContent: %v", err)
	}
	if got.count != payloadSize {
		t.Fatalf("passthrough size = %d, want %d", got.count, payloadSize)
	}
	wantHash := sha256.New()
	chunk := bytes.Repeat([]byte{'x'}, 32*1024)
	for remaining := payloadSize; remaining > 0; {
		n := len(chunk)
		if n > remaining {
			n = remaining
		}
		_, _ = wantHash.Write(chunk[:n])
		remaining -= n
	}
	if !bytes.Equal(got.hash.Sum(nil), wantHash.Sum(nil)) {
		t.Fatal("passthrough checksum differs from input")
	}
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("smudged %d-byte nonpointer with %d allocated bytes", payloadSize, allocated)
	if allocated >= payloadSize/2 {
		t.Fatalf("allocated %d bytes for %d-byte passthrough, want less than %d", allocated, payloadSize, payloadSize/2)
	}
}

type repeatingReader struct {
	remaining int
	value     byte
}

func (r *repeatingReader) Read(dst []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(dst)
	if n > r.remaining {
		n = r.remaining
	}
	for i := 0; i < n; i++ {
		dst[i] = r.value
	}
	r.remaining -= n
	return n, nil
}

func TestSmudgeContentUsesCacheWhenPresent(t *testing.T) {
	repo := setupSmudgeTestRepo(t)
	oid := checksumForTestContent("cached-content")
	cachePath := mustObjectPath(t, oid)
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("cached-content"), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer

	err := SmudgeContent(context.Background(), filepath.Join(repo, "data.txt"), bytes.NewBufferString(pointerForOID(oid, 14)), &out, logger, nil)
	if err != nil {
		t.Fatalf("SmudgeContent returned error: %v", err)
	}
	if got := out.String(); got != "cached-content" {
		t.Fatalf("cache hit output = %q, want cached payload", got)
	}
}

func TestSmudgeContentDownloadsWhenCacheMiss(t *testing.T) {
	oid := checksumForTestContent("downloaded-bytes")
	setupSmudgeTestRepo(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var out bytes.Buffer
	called := 0

	err := SmudgeContent(context.Background(), "file.bin", bytes.NewBufferString(pointerForOID(oid, 16)), &out, logger, func(ctx context.Context, gotOID, cachePath string) error {
		called++
		if gotOID != oid {
			return errors.New("unexpected oid")
		}
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(cachePath, []byte("downloaded-bytes"), 0o644)
	})
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

func TestSmudgeContentReplacesCorruptCache(t *testing.T) {
	const payload = "correct-payload"
	oid := checksumForTestContent(payload)
	objectsRoot := filepath.Join(t.TempDir(), "objects")
	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("xxxxxxxxxxxxxxx"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	called := false
	err = SmudgeContentWithObjectsRoot(t.Context(), objectsRoot, "file.bin", strings.NewReader(pointerForOID(oid, int64(len(payload)))), &out, slog.New(slog.NewTextHandler(io.Discard, nil)), func(_ context.Context, _ string, destination string) error {
		called = true
		if _, err := os.Stat(destination); !os.IsNotExist(err) {
			return fmt.Errorf("corrupt cache still exists before download: %v", err)
		}
		return os.WriteFile(destination, []byte(payload), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called || out.String() != payload {
		t.Fatalf("corrupt cache was not replaced: called=%v output=%q", called, out.String())
	}
}

func TestSmudgeContentHydratesPlaceholderPointer(t *testing.T) {
	const payload = "real payload"
	placeholder := checksumForTestContent("provider metadata")
	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\next-0-gitdrsplaceholder sha256:%s\noid sha256:%s\nsize %d\n", placeholder, placeholder, len(payload))
	objectsRoot := filepath.Join(t.TempDir(), "objects")
	var output bytes.Buffer
	if err := SmudgeContentWithObjectsRoot(t.Context(), objectsRoot, "file.bin", strings.NewReader(pointer), &output, nil, func(_ context.Context, _ string, destination string) error {
		return os.WriteFile(destination, []byte(payload), 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	if output.String() != payload {
		t.Fatalf("smudged payload = %q, want %q", output.String(), payload)
	}
}

func TestSmudgeContentHydratesPointerLargerThanTwoKiB(t *testing.T) {
	const payload = "cached payload"
	oid := checksumForTestContent(payload)
	objectsRoot := filepath.Join(t.TempDir(), "objects")
	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}

	pointer := fmt.Sprintf("version https://git-lfs.github.com/spec/v1\next-1-test %s\noid sha256:%s\nsize %d\n", strings.Repeat("x", 3*1024), oid, len(payload))
	if len(pointer) <= 2*1024 {
		t.Fatalf("test pointer size = %d, want more than 2 KiB", len(pointer))
	}
	if _, _, ok := lfs.ParseLFSPointer([]byte(pointer)); !ok {
		t.Fatal("test pointer is not accepted by the LFS pointer parser")
	}

	var output bytes.Buffer
	err = SmudgeContentWithObjectsRoot(t.Context(), objectsRoot, "file.bin", strings.NewReader(pointer), &output, nil, func(context.Context, string, string) error {
		return errors.New("cache hit should not download")
	})
	if err != nil {
		t.Fatalf("SmudgeContentWithObjectsRoot: %v", err)
	}
	if output.String() != payload {
		t.Fatalf("smudged output = %q, want cached payload %q", output.String(), payload)
	}
}

func checksumForTestContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func setupSmudgeTestRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
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
	path, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
	if err != nil {
		t.Fatalf("ObjectPath: %v", err)
	}
	return path
}

func pointerForOID(oid string, size int64) string {
	return fmt.Sprintf("version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n", oid, size)
}
