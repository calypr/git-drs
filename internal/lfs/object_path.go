package lfs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

var sha256OIDRe = regexp.MustCompile(`(?i)^[a-f0-9]{64}$`)

// ObjectPath returns the Git LFS fanout path for an object ID.
//
// The cache layout remains SHA256-shaped for compatibility with existing LFS
// object storage. Git LFS pointers pass their sha256 object IDs through
// unchanged, while git-drs DRS URI pointers are assigned a deterministic local
// cache key derived from the normalized DRS URI.
func ObjectPath(basePath string, oid string) (string, error) {
	cacheKey, err := cacheKeyForOID(oid)
	if err != nil {
		return "", err
	}

	return filepath.Join(basePath, cacheKey[:2], cacheKey[2:4], cacheKey), nil
}

func cacheKeyForOID(oid string) (string, error) {
	oid = strings.TrimSpace(oid)
	oid = strings.TrimPrefix(oid, "sha256:")
	if sha256OIDRe.MatchString(oid) {
		return strings.ToLower(oid), nil
	}

	if isDRSURI(oid) {
		sum := sha256.Sum256([]byte("git-drs-anvil-ref:v1\n" + normalizeDRSURI(oid)))
		return hex.EncodeToString(sum[:]), nil
	}

	return "", fmt.Errorf("error: %s is not a valid sha256 hash or DRS URI", oid)
}

func isDRSURI(uri string) bool {
	uri = strings.TrimSpace(uri)
	return strings.HasPrefix(strings.ToLower(uri), "drs://") || strings.HasPrefix(uri, "//")
}

func normalizeDRSURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if strings.HasPrefix(uri, "//") {
		return "drs:" + uri
	}
	if len(uri) >= len("drs://") && strings.EqualFold(uri[:len("drs://")], "drs://") {
		return "drs://" + uri[len("drs://"):]
	}
	return uri
}
