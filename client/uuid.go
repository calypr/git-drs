package client

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// AUTHORITY is the canonical authority for Git-DRS UUIDs
// Used to derive the NAMESPACE UUID, but NOT included in the canonical DID string
// for UUID generation. The canonical format is: did:gen3:path:sha256:size
const AUTHORITY = "https://calypr.org"

// NAMESPACE is the deterministic namespace UUID for all Git-DRS UUIDs
// Derived from DNS namespace and AUTHORITY to ensure global uniqueness and reproducibility
// This matches the pattern: uuid.uuid3(uuid.NAMESPACE_DNS, AUTHORITY)
// Where AUTHORITY = "https://calypr.org"
var NAMESPACE = uuid.NewMD5(uuid.NameSpaceDNS, []byte(AUTHORITY))

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
// This follows the specification with collection-aware namespacing:
// Canonical format: did:gen3:{collection}:{logical_path}:{sha256}
// UUID = UUIDv5(NAMESPACE, canonical_string)
//
// The NAMESPACE is derived from AUTHORITY but the AUTHORITY itself is NOT included
// in the canonical string used for UUID generation.
//
// The collection parameter provides project-level isolation to prevent UUID collisions:
//   - Gen3: collection = "program-project" (e.g., "gdc-tcga")
//   - Other DRS: collection = bucket_name or workspace_id or study_id
//   - Standalone: collection = repo_name or empty string
//
// Parameters:
//   - collection: The collection/namespace identifier (e.g., project ID, bucket name)
//   - logicalPath: The repository-relative path to the file (will be normalized)
//   - sha256: The SHA256 hash of the file (lowercase hex string)
//
// Returns:
//   - A deterministic UUID string that can be reproduced by any tool with the same inputs
//
// Example:
//
//	uuid := ComputeDeterministicUUID("gdc-tcga", "/data/sample.fastq", "abc123...")
//	// Returns: "8f6f3f44-2a51-5e7d-b8d6-1f2a1b0c9d77" (deterministic)
func ComputeDeterministicUUID(collection, logicalPath, sha256 string) string {
	// Normalize inputs
	normalizedPath := NormalizeLogicalPath(logicalPath)
	normalizedSHA256 := strings.ToLower(sha256)

	// Build canonical DID string (WITHOUT authority in the string itself)
	// Format: did:gen3:{collection}:{path}:{sha256}
	canonical := fmt.Sprintf("did:gen3:%s:%s:%s",
		collection,
		normalizedPath,
		normalizedSHA256)

	// Generate UUIDv5 (SHA1-based) from canonical string
	return uuid.NewSHA1(NAMESPACE, []byte(canonical)).String()
}
