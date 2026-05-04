package lfs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ObjectPath returns the Git LFS fanout path for a sha256 object ID.
func ObjectPath(basePath string, oid string) (string, error) {
	oid = strings.TrimPrefix(oid, "sha256:")
	if len(oid) != 64 {
		return "", fmt.Errorf("error: %s is not a valid sha256 hash", oid)
	}

	return filepath.Join(basePath, oid[:2], oid[2:4], oid), nil
}
