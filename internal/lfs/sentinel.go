package lfs

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const addURLSentinelHeader = "git-drs-add-url-sentinel:v1\n"

func SyntheticOIDFromETag(etag string) (string, error) {
	e := strings.TrimSpace(strings.Trim(etag, `"`))
	if e == "" {
		return "", fmt.Errorf("etag is required for synthetic oid")
	}
	sum := sha256.Sum256([]byte(e))
	return fmt.Sprintf("%x", sum[:]), nil
}

func BuildAddURLSentinel(etag string, sourceURL string) ([]byte, error) {
	e := strings.TrimSpace(strings.Trim(etag, `"`))
	if e == "" {
		return nil, fmt.Errorf("etag is required for sentinel")
	}
	return []byte(addURLSentinelHeader + "etag=" + e + "\nsource=" + strings.TrimSpace(sourceURL) + "\n"), nil
}

func IsAddURLSentinelBytes(data []byte) bool {
	return strings.HasPrefix(string(data), addURLSentinelHeader)
}

func IsAddURLSentinelObject(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, len(addURLSentinelHeader))
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false, err
	}
	return IsAddURLSentinelBytes(buf[:n]), nil
}

func WriteAddURLSentinelObject(lfsRoot string, oid string, etag string, sourceURL string) (string, error) {
	objPath := filepath.Join(lfsRoot, "objects", oid[:2], oid[2:4], oid)
	if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(objPath), err)
	}
	payload, err := BuildAddURLSentinel(etag, sourceURL)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(objPath, payload, 0o644); err != nil {
		return "", fmt.Errorf("write sentinel %s: %w", objPath, err)
	}
	return objPath, nil
}
