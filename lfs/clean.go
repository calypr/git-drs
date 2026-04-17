package lfs

// Package filterops contains the shared logic for the git-drs clean and
// filter-process sub commands. Both sub commands need to convert raw file
// content into an LFS pointer (the "clean" side of the LFS filter pair), so
// the implementation lives here and is imported by each command rather than
// being duplicated.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/git-drs/common"
	datadrs "github.com/calypr/syfon/client/drs"
)

// ParseLFSPointer extracts the oid and size from an LFS pointer payload.
// Returns ("", 0, false) if data is not a valid LFS pointer.
func ParseLFSPointer(data []byte) (oid string, size int64, ok bool) {
	text := string(data)
	if !strings.Contains(text, "version https://git-lfs.github.com/spec/v1") {
		return "", 0, false
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "oid sha256:") {
			oid = strings.TrimPrefix(line, "oid sha256:")
		}
		if strings.HasPrefix(line, "size ") {
			fmt.Sscanf(strings.TrimPrefix(line, "size "), "%d", &size)
		}
	}
	if oid == "" {
		return "", 0, false
	}
	return oid, size, true
}

// WriteDrsMap records a local DRS object entry in .git/drs/lfs/objects so that
// the pre-push workflow can discover and upload the file. This mirrors the
// ObjectStore.WriteObject pattern.
func WriteDrsMap(pathname string, oid string, size int64) error {
	drsObj := &datadrs.DRSObject{
		Name: filepath.Base(pathname),
		Size: size,
		Checksums: []datadrs.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}
	return WriteObject(common.DRS_OBJS_PATH, drsObj, oid)
}

// CleanContent reads raw file content from content, hashes it with SHA-256,
// stores the content in the git-lfs local object cache under lfsRoot, and
// writes an LFS pointer to dst. It also records a DRS map entry so that
// `git drs push` can discover the file.
//
// pathname is the repo-relative path of the file being cleaned; it is used
// only for the DRS map entry name and log messages.
func CleanContent(ctx context.Context, lfsRoot, pathname string, content io.Reader, dst io.Writer, logger *slog.Logger) error {
	_ = ctx // reserved for future cancellation propagation

	objDir := filepath.Join(lfsRoot, "objects")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		return fmt.Errorf("clean: mkdir LFS objects: %w", err)
	}

	// Buffer the content into a temp file while computing its SHA-256.
	tmp, err := os.CreateTemp(objDir, "git-drs-clean-*")
	if err != nil {
		return fmt.Errorf("clean: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	written, err := io.Copy(tmp, io.TeeReader(content, h))
	if err != nil {
		tmp.Close()
		return fmt.Errorf("clean: write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("clean: close temp file: %w", err)
	}
	size := written
	oid := hex.EncodeToString(h.Sum(nil))

	// Move temp file to the final content-addressed location.
	cachePath, err := ObjectPath(common.LFS_OBJS_PATH, oid)
	if err != nil {
		return fmt.Errorf("clean: resolve cache path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("clean: mkdir for cache path: %w", err)
	}
	if err := os.Rename(tmpPath, cachePath); err != nil {
		return fmt.Errorf("clean: move to cache: %w", err)
	}

	logger.Debug("clean: stored LFS object", "pathname", pathname, "oid", oid, "size", size)

	// Write the LFS pointer to dst.
	pointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		oid, size,
	)
	if _, err := io.WriteString(dst, pointer); err != nil {
		return fmt.Errorf("clean: write pointer: %w", err)
	}

	// Record a DRS map entry so `git drs push` can find the file.
	if mapErr := WriteDrsMap(pathname, oid, size); mapErr != nil {
		logger.Warn("clean: failed to write DRS map entry", "pathname", pathname, "error", mapErr)
	}

	return nil
}
