package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

const standardLFSPointerVersion = "version https://git-lfs.github.com/spec/v1"

func bytesHaveLFSVersionPrefix(value []byte) bool {
	return len(value) >= len("version ") && string(value[:len("version ")]) == "version "
}

// ParseLFSPointer accepts canonical three-line Git-LFS SHA-256 pointers.
// It returns false without an error for content that has no pointer header.
func ParseLFSPointer(content []byte) (File, bool, error) {
	if !bytesHaveLFSVersionPrefix(content) {
		return File{}, false, nil
	}
	text := string(content)
	if strings.ContainsRune(text, '\r') {
		return File{}, false, errors.New("Git-LFS pointer must use canonical LF line endings")
	}
	if !strings.HasPrefix(text, standardLFSPointerVersion+"\n") {
		return File{}, false, errors.New("unsupported or malformed Git-LFS pointer version")
	}
	text = strings.TrimSuffix(text, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) != 3 || lines[0] != standardLFSPointerVersion || !strings.HasPrefix(lines[1], "oid sha256:") || !strings.HasPrefix(lines[2], "size ") {
		return File{}, false, errors.New("only three-line Git-LFS SHA-256 pointers are supported; extensions, placeholders, and DRS-URI pointers are unsupported")
	}
	oidHex := strings.TrimPrefix(lines[1], "oid sha256:")
	if len(oidHex) != sha256.Size*2 {
		return File{}, false, errors.New("Git-LFS pointer OID must be a full SHA-256 hash")
	}
	if _, err := hex.DecodeString(oidHex); err != nil {
		return File{}, false, errors.New("Git-LFS pointer OID must be hexadecimal")
	}
	sizeText := strings.TrimPrefix(lines[2], "size ")
	if sizeText == "" || strings.HasPrefix(sizeText, "+") || strings.HasPrefix(sizeText, "-") {
		return File{}, false, errors.New("Git-LFS pointer size must be a nonnegative decimal integer")
	}
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size < 0 || strconv.FormatInt(size, 10) != sizeText {
		return File{}, false, errors.New("Git-LFS pointer size must be a nonnegative decimal integer")
	}
	return File{Path: "", OID: "sha256:" + strings.ToLower(oidHex), Size: size}, true, nil
}
