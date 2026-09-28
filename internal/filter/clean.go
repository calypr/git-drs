package filter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/calypr/git-drs/internal/drsobject"
	"github.com/calypr/git-drs/internal/gitrepo"
	"github.com/calypr/git-drs/internal/lfs"
	drsapi "github.com/calypr/syfon/apigen/drs"
)

const IndexRefreshEnv = "GIT_DRS_INDEX_REFRESH"

func writeDrsMap(objectsRoot, pathname string, oid string, size int64) error {
	name := filepath.Base(pathname)
	drsObj := &drsapi.DrsObject{
		Name: &name,
		Size: size,
		Checksums: []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		},
	}
	if existing, err := drsobject.ReadObject(objectsRoot, oid); err == nil && existing != nil {
		drsObj = existing
		drsObj.Name = &name
		drsObj.Size = size
		drsObj.Checksums = []drsapi.Checksum{
			{Type: "sha256", Checksum: oid},
		}
	}
	return drsobject.WriteObject(objectsRoot, drsObj, oid)
}

func CleanContent(ctx context.Context, lfsRoot, pathname string, content io.Reader, dst io.Writer, logger *slog.Logger) (retErr error) {
	objectsRoot, err := gitrepo.ResolveDRSObjectsDir(ctx)
	if err != nil {
		return fmt.Errorf("clean: resolve DRS object store: %w", err)
	}
	return CleanContentWithRoots(ctx, lfsRoot, objectsRoot, pathname, content, dst, logger)
}

func CleanContentWithRoots(ctx context.Context, lfsRoot, drsObjectsRoot, pathname string, content io.Reader, dst io.Writer, logger *slog.Logger) (retErr error) {
	if os.Getenv(IndexRefreshEnv) == "1" {
		return cleanIndexedPointerForRefresh(ctx, lfsRoot, pathname, content, dst)
	}
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

	// A DRS pointer can use the durable DRS URI as its oid rather than the
	// payload checksum. When Git asks the clean filter to inspect a hydrated
	// file, preserve that indexed pointer if its recorded checksum proves the
	// worktree payload is unchanged. Otherwise hydration would turn the URI
	// pointer into a SHA256 LFS pointer and appear as a staged modification.
	if pointer, ok := matchingIndexedDRSPointer(pathname, oid, size, lfsRoot); ok {
		if _, err := dst.Write(pointer); err != nil {
			return fmt.Errorf("clean: write indexed DRS pointer: %w", err)
		}
		logger.Debug("clean: restored indexed DRS pointer for hydrated content", "pathname", pathname, "oid", oid, "size", size)
		return nil
	}

	if size > 0 && size < 2048 {
		if data, readErr := os.ReadFile(tmpPath); readErr == nil {
			if pointerOID, pointerSize, ok := lfs.ParseLFSPointer(data); ok {
				if _, err := dst.Write(data); err != nil {
					return fmt.Errorf("clean: write existing pointer: %w", err)
				}
				// DRS URI pointers already carry their durable lookup identity. The
				// SHA256-keyed sidecar map is only applicable to SHA256 pointers.
				if !lfs.IsDRSURI(pointerOID) && !lfs.IsPlaceholderPointer(data) {
					if mapErr := writeDrsMap(drsObjectsRoot, pathname, pointerOID, pointerSize); mapErr != nil {
						logger.Warn("clean: failed to write DRS map entry for existing pointer", "pathname", pathname, "error", mapErr)
					}
				}
				logger.Debug("clean: passed through existing LFS pointer", "pathname", pathname, "oid", pointerOID, "size", pointerSize)
				return nil
			}
		}
	}

	cachePath, err := lfs.ObjectPath(objDir, oid)
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

	if mapErr := writeDrsMap(drsObjectsRoot, pathname, oid, size); mapErr != nil {
		logger.Warn("clean: failed to write DRS map entry", "pathname", pathname, "error", mapErr)
	}

	return nil
}

func cleanIndexedPointerForRefresh(ctx context.Context, lfsRoot, pathname string, content io.Reader, dst io.Writer) error {
	pathname = filepath.ToSlash(filepath.Clean(pathname))
	if pathname == "." || pathname == ".." || strings.HasPrefix(pathname, "../") {
		return fmt.Errorf("refresh index: invalid path %q", pathname)
	}
	pointer, err := exec.CommandContext(ctx, "git", "show", ":"+pathname).Output()
	if err != nil {
		return fmt.Errorf("refresh index: read indexed pointer for %s: %w", pathname, err)
	}
	pointerOID, pointerSize, ok := lfs.ParseLFSPointer(pointer)
	if !ok {
		return fmt.Errorf("refresh index: %s is not an indexed DRS pointer", pathname)
	}
	h := sha256.New()
	size, err := io.Copy(h, content)
	if err != nil {
		return fmt.Errorf("refresh index: read %s: %w", pathname, err)
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if size != pointerSize {
		return fmt.Errorf("refresh index: %s changed size during checkout", pathname)
	}
	if lfs.IsDRSURI(pointerOID) || lfs.IsPlaceholderPointer(pointer) {
		cachedOID, ok := cachedObjectOID(pointerOID, size, lfsRoot)
		if !ok || cachedOID != actual {
			return fmt.Errorf("refresh index: %s changed after checkout", pathname)
		}
	} else if !strings.EqualFold(pointerOID, actual) {
		return fmt.Errorf("refresh index: %s changed after checkout", pathname)
	}
	if _, err := dst.Write(pointer); err != nil {
		return fmt.Errorf("refresh index: write pointer for %s: %w", pathname, err)
	}
	return nil
}

func matchingIndexedDRSPointer(pathname, contentOID string, size int64, lfsRoot string) ([]byte, bool) {
	pathname = filepath.ToSlash(filepath.Clean(pathname))
	if pathname == "." || pathname == ".." || strings.HasPrefix(pathname, "../") {
		return nil, false
	}
	cmd := exec.Command("git", "show", ":"+pathname)
	pointer, err := cmd.Output()
	if err != nil {
		return nil, false
	}
	pointerOID, pointerSize, ok := lfs.ParseLFSPointer(pointer)
	placeholder := lfs.IsPlaceholderPointer(pointer)
	if !ok || (!lfs.IsDRSURI(pointerOID) && !placeholder) || pointerSize != size {
		return nil, false
	}
	// Some DRS services do not publish a SHA256 checksum. In that case compare
	// the worktree payload with the validated object cached by pull. This keeps
	// status clean without hiding a same-sized edit made after hydration.
	if cachedOID, ok := cachedObjectOID(pointerOID, size, lfsRoot); ok && cachedOID == contentOID {
		return pointer, true
	}
	for _, line := range strings.Split(string(pointer), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.EqualFold(fields[0], "sha256") {
			checksum := strings.TrimPrefix(strings.ToLower(fields[1]), "sha256:")
			if checksum == contentOID {
				return pointer, true
			}
		}
		if len(fields) == 2 && fields[0] == "size" {
			// Reject malformed sizes even if a future pointer parser becomes
			// more permissive.
			if parsed, err := strconv.ParseInt(fields[1], 10, 64); err != nil || parsed != size {
				return nil, false
			}
		}
	}
	return nil, false
}

func cachedObjectOID(pointerOID string, size int64, lfsRoot string) (string, bool) {
	cachePath, err := lfs.ObjectPath(filepath.Join(lfsRoot, "objects"), pointerOID)
	if err != nil {
		return "", false
	}
	f, err := os.Open(cachePath)
	if err != nil {
		return "", false
	}
	defer f.Close()
	h := sha256.New()
	written, err := io.Copy(h, f)
	if err != nil || written != size {
		return "", false
	}
	return hex.EncodeToString(h.Sum(nil)), true
}
