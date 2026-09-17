package lfs

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
)

// FileMatchesPointer reports whether path contains a complete payload for the
// supplied pointer. SHA-256 pointers are checked against their OID; DRS
// pointers use their optional sha256 extension and otherwise fall back to the
// authoritative size recorded by the pointer.
func FileMatchesPointer(path string, pointerData []byte) (bool, error) {
	pointer, ok := parseLFSPointer(string(pointerData))
	if !ok {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.IsDir() || info.Size() != pointer.Size {
		return false, nil
	}
	want := pointer.SHA256
	if pointer.OidType == "sha256" {
		want = pointer.Oid
	}
	if want == "" {
		return true, nil
	}
	return FileMatchesSHA256(path, want)
}

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
