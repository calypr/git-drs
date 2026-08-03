package filter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
)

type SmudgeDownloadFunc func(ctx context.Context, oid, cachePath string) error

func SmudgeContent(ctx context.Context, pathname string, ptr io.Reader, dst io.Writer, logger *slog.Logger, download SmudgeDownloadFunc) error {
	ptrBytes, err := io.ReadAll(ptr)
	if err != nil {
		return fmt.Errorf("smudge: read pointer: %w", err)
	}

	oid, size, ok := lfs.ParseLFSPointer(ptrBytes)
	if !ok {
		_, err := dst.Write(ptrBytes)
		if err != nil {
			return fmt.Errorf("smudge: passthrough write: %w", err)
		}
		return nil
	}

	if logger != nil {
		logger.Debug("smudge", "pathname", pathname, "oid", oid, "size", size)
	}

	cachePath, err := lfs.ObjectPath(gitrepo.LFSObjectsPath, oid)
	if err != nil {
		return fmt.Errorf("smudge: resolve cache path: %w", err)
	}

	err = copyObjectToWriter(cachePath, dst)
	if err == nil {
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("smudge: read cache: %w", err)
	}

	if download == nil {
		if logger != nil {
			logger.Debug("smudge: no downloader configured, writing pointer", "oid", oid)
		}
		_, err := dst.Write(ptrBytes)
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return fmt.Errorf("smudge: mkdir for cache path: %w", err)
	}

	if err := download(ctx, oid, cachePath); err != nil {
		return fmt.Errorf("smudge: download oid %s: %w", oid, err)
	}

	if err := copyObjectToWriter(cachePath, dst); err != nil {
		return fmt.Errorf("smudge: open downloaded file: %w", err)
	}

	return nil
}

func copyObjectToWriter(path string, dst io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(dst, f)
	return err
}
