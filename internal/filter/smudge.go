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
	return SmudgeContentWithObjectsRoot(ctx, gitrepo.LFSObjectsPath, pathname, ptr, dst, logger, download)
}

func SmudgeContentWithObjectsRoot(ctx context.Context, objectsRoot, pathname string, ptr io.Reader, dst io.Writer, logger *slog.Logger, download SmudgeDownloadFunc) error {
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

	cachePath, err := lfs.ObjectPath(objectsRoot, oid)
	if err != nil {
		return fmt.Errorf("smudge: resolve cache path: %w", err)
	}

	valid, validateErr := lfs.FileMatchesPointer(cachePath, ptrBytes)
	if validateErr == nil && valid {
		return copyObjectToWriter(cachePath, dst)
	}
	if validateErr != nil && !errors.Is(validateErr, fs.ErrNotExist) {
		return fmt.Errorf("smudge: validate cache: %w", validateErr)
	}
	if validateErr == nil && !valid {
		if err := os.Remove(cachePath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("smudge: remove invalid cache: %w", err)
		}
		_ = os.Remove(cachePath + ".syfon-download.json")
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

	valid, err = lfs.FileMatchesPointer(cachePath, ptrBytes)
	if err != nil {
		return fmt.Errorf("smudge: validate downloaded cache: %w", err)
	}
	if !valid {
		return fmt.Errorf("smudge: downloaded cache does not match oid or size")
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
