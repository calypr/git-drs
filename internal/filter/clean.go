package filter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/client/drs"
)

func writeDrsMap(pathname string, oid string, size int64) error {
	name := filepath.Base(pathname)
	drsObj := &drsapi.DrsObject{
		Name: &name,
		Size: size,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}
	if existing, err := drsobject.ReadObject(gitrepo.DRSObjectsPath, oid); err == nil && existing != nil {
		drsObj = existing
		drsObj.Name = &name
		drsObj.Size = size
		drsObj.Checksums = []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		}
	}
	return drsobject.WriteObject(gitrepo.DRSObjectsPath, drsObj, oid)
}

func CleanContent(_ context.Context, lfsRoot, pathname string, content io.Reader, dst io.Writer, logger *slog.Logger) (retErr error) {
	objDir := filepath.Join(lfsRoot, "objects")
	if err := os.MkdirAll(objDir, 0o755); err != nil {
		return fmt.Errorf("clean: mkdir LFS objects: %w", err)
	}

	tmp, err := os.CreateTemp(objDir, "git-drs-clean-*")
	if err != nil {
		return fmt.Errorf("clean: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			if rmErr := os.Remove(tmpPath); rmErr != nil && retErr == nil {
				retErr = fmt.Errorf("clean: remove temp file: %w", rmErr)
			}
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

	if size > 0 && size < 2048 {
		if data, readErr := os.ReadFile(tmpPath); readErr == nil {
			if pointerOID, pointerSize, ok := lfs.ParseLFSPointer(data); ok {
				if _, err := dst.Write(data); err != nil {
					return fmt.Errorf("clean: write existing pointer: %w", err)
				}
				if mapErr := writeDrsMap(pathname, pointerOID, pointerSize); mapErr != nil {
					logger.Warn("clean: failed to write DRS map entry for existing pointer", "pathname", pathname, "error", mapErr)
				}
				logger.Debug("clean: passed through existing LFS pointer", "pathname", pathname, "oid", pointerOID, "size", pointerSize)
				return nil
			}
		}
	}

	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
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

	pointer := fmt.Sprintf(
		"version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %d\n",
		oid, size,
	)
	if _, err := io.WriteString(dst, pointer); err != nil {
		return fmt.Errorf("clean: write pointer: %w", err)
	}

	if mapErr := writeDrsMap(pathname, oid, size); mapErr != nil {
		logger.Warn("clean: failed to write DRS map entry", "pathname", pathname, "error", mapErr)
	}

	return nil
}
