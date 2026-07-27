// Package inventory exposes immutable Git-DRS pointer facts for a Git ref.
package inventory

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/calypr/git-drs/internal/drslog"
	"github.com/calypr/git-drs/internal/lfs"
)

// Pointer is one Git-LFS/Git-DRS pointer stored in a Git tree.
type Pointer struct {
	Path   string
	SHA256 string
	Size   int64
}

// Options selects a repository snapshot and optional repository-relative path
// prefixes to omit from its pointer inventory.
type Options struct {
	RepositoryRoot  string
	Ref             string
	ExcludePrefixes []string
}

// List returns valid SHA-256 LFS pointers from the selected Git ref. It reads
// Git blobs, not working-tree files, and therefore works with hydrated trees.
func List(ctx context.Context, options Options) ([]Pointer, error) {
	files, err := lfs.GetReachablePointerFilesForRefInRepository(ctx, options.RepositoryRoot, options.Ref, drslog.NewNoOpLogger())
	if err != nil {
		return nil, fmt.Errorf("list Git-DRS pointers: %w", err)
	}
	prefixes := normalizePrefixes(options.ExcludePrefixes)
	pointers := make([]Pointer, 0, len(files))
	for path, file := range files {
		sha256 := strings.TrimSpace(file.SHA256)
		if sha256 == "" && file.OidType == "sha256" {
			sha256 = strings.TrimSpace(file.Oid)
		}
		if file.OidType != "sha256" || sha256 == "" || excluded(path, prefixes) {
			continue
		}
		pointers = append(pointers, Pointer{
			Path:   filepath.ToSlash(path),
			SHA256: strings.ToLower(sha256),
			Size:   file.Size,
		})
	}
	sort.Slice(pointers, func(i, j int) bool {
		return pointers[i].Path < pointers[j].Path
	})
	return pointers, nil
}

func normalizePrefixes(prefixes []string) []string {
	out := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.Trim(filepath.ToSlash(filepath.Clean(strings.TrimSpace(prefix))), "/")
		if prefix != "" && prefix != "." {
			out = append(out, prefix)
		}
	}
	return out
}

func excluded(path string, prefixes []string) bool {
	path = strings.Trim(filepath.ToSlash(path), "/")
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
