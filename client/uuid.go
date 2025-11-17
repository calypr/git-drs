package client

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// AUTHORITY is the canonical authority for Git-DRS UUIDs
// Used in the DID format: did:gen3:AUTHORITY:path:sha256:size
const AUTHORITY = "calypr.org"

// ACED_NAMESPACE is the deterministic namespace UUID for all Git-DRS UUIDs
// Derived from DNS namespace to ensure global uniqueness and reproducibility
// This matches the pattern: uuid.uuid3(uuid.NAMESPACE_DNS, b'aced-idp.org')
var ACED_NAMESPACE = uuid.NewMD5(uuid.NameSpaceDNS, []byte("aced-idp.org"))

// NormalizeLogicalPath normalizes a file path to ensure consistent UUID generation
// Rules:
//   - Convert to forward slashes (POSIX style)
//   - Remove duplicate slashes
//   - Remove trailing slash (unless root)
//   - Ensure leading slash
func NormalizeLogicalPath(path string) string {
	// Explicitly convert backslashes to forward slashes
	// (filepath.ToSlash doesn't work on Linux where backslash is a valid filename character)
	path = strings.ReplaceAll(path, "\\", "/")

	// Remove duplicate slashes
	re := regexp.MustCompile(`/+`)
	path = re.ReplaceAllString(path, "/")

	// Remove trailing slash unless root
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	// Ensure leading slash
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return path
}

// ComputeDeterministicUUID generates a deterministic UUID based on the canonical DID string
// This follows the specification:
// Canonical format: did:gen3:{authority}:{logical_path}:{sha256}:{size}
// UUID = UUIDv5(ACED_NAMESPACE, canonical_string)
//
// Parameters:
//   - logicalPath: The repository-relative path to the file (will be normalized)
//   - sha256: The SHA256 hash of the file (lowercase hex string)
//   - size: The size of the file in bytes
//
// Returns:
//   - A deterministic UUID string that can be reproduced by any tool with the same inputs
//
// Example:
//
//	uuid := ComputeDeterministicUUID("/data/sample.fastq", "abc123...", 1024000)
//	// Returns: "8f6f3f44-2a51-5e7d-b8d6-1f2a1b0c9d77" (deterministic)
func ComputeDeterministicUUID(logicalPath, sha256 string, size int64) string {
	// Normalize inputs
	normalizedPath := NormalizeLogicalPath(logicalPath)
	normalizedSHA256 := strings.ToLower(sha256)

	// Build canonical DID string
	canonical := fmt.Sprintf("did:gen3:%s:%s:%s:%d",
		AUTHORITY,
		normalizedPath,
		normalizedSHA256,
		size)

	// Generate UUIDv5 (SHA1-based) from canonical string
	return uuid.NewSHA1(ACED_NAMESPACE, []byte(canonical)).String()
}
