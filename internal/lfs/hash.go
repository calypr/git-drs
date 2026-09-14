package lfs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// FileMatchesSHA256 reports whether path contains the requested SHA-256 bytes.
func FileMatchesSHA256(path, oid string) (bool, error) {
	want := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(oid), "sha256:"))
	if len(want) != sha256.Size*2 {
		return false, nil
	}
	if _, err := hex.DecodeString(want); err != nil {
		return false, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hasher.Sum(nil)) == want, nil
}
