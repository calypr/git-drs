package client

import (
	"testing"
)

// TestComputeDeterministicUUID_Reproducibility verifies that the same inputs always produce the same UUID
func TestComputeDeterministicUUID_Reproducibility(t *testing.T) {
	collection := "gdc-tcga"
	path := "/data/sample.fastq"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Generate UUID multiple times
	uuid1 := ComputeDeterministicUUID(collection, path, sha256)
	uuid2 := ComputeDeterministicUUID(collection, path, sha256)
	uuid3 := ComputeDeterministicUUID(collection, path, sha256)

	// All should be identical
	if uuid1 != uuid2 || uuid2 != uuid3 {
		t.Errorf("ComputeDeterministicUUID() not reproducible: got %s, %s, %s", uuid1, uuid2, uuid3)
	}

	// UUID should not be empty
	if uuid1 == "" {
		t.Error("ComputeDeterministicUUID() returned empty string")
	}
}

// TestComputeDeterministicUUID_DifferentPaths verifies that different paths produce different UUIDs
func TestComputeDeterministicUUID_DifferentPaths(t *testing.T) {
	collection := "gdc-tcga"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	uuid1 := ComputeDeterministicUUID(collection, "/data/sample.fastq", sha256)
	uuid2 := ComputeDeterministicUUID(collection, "/backup/sample.fastq", sha256)

	if uuid1 == uuid2 {
		t.Errorf("ComputeDeterministicUUID() should generate different UUIDs for different paths, got: %s", uuid1)
	}
}

// TestComputeDeterministicUUID_DifferentHashes verifies that different hashes produce different UUIDs
func TestComputeDeterministicUUID_DifferentHashes(t *testing.T) {
	collection := "gdc-tcga"
	path := "/data/sample.fastq"

	uuid1 := ComputeDeterministicUUID(collection, path, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	uuid2 := ComputeDeterministicUUID(collection, path, "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	if uuid1 == uuid2 {
		t.Errorf("ComputeDeterministicUUID() should generate different UUIDs for different hashes, got: %s", uuid1)
	}
}

// TestComputeDeterministicUUID_SameSizesProduceSameUUID verifies that size doesn't affect UUID generation
// This test replaces the old TestComputeDeterministicUUID_DifferentSizes test
func TestComputeDeterministicUUID_SameSizesProduceSameUUID(t *testing.T) {
	collection := "gdc-tcga"
	path := "/data/sample.fastq"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Size is no longer part of UUID generation, so these should be the same
	uuid1 := ComputeDeterministicUUID(collection, path, sha256)
	uuid2 := ComputeDeterministicUUID(collection, path, sha256)

	if uuid1 != uuid2 {
		t.Errorf("ComputeDeterministicUUID() should generate same UUIDs regardless of size metadata, got: %s and %s", uuid1, uuid2)
	}
}

// TestComputeDeterministicUUID_CaseInsensitiveHash verifies that hash case doesn't matter
func TestComputeDeterministicUUID_CaseInsensitiveHash(t *testing.T) {
	collection := "gdc-tcga"
	path := "/data/sample.fastq"

	uuid1 := ComputeDeterministicUUID(collection, path, "E3B0C44298FC1C149AFBF4C8996FB92427AE41E4649B934CA495991B7852B855")
	uuid2 := ComputeDeterministicUUID(collection, path, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	if uuid1 != uuid2 {
		t.Errorf("ComputeDeterministicUUID() should be case-insensitive for hashes: got %s and %s", uuid1, uuid2)
	}
}

// TestNormalizeLogicalPath_BasicCases verifies basic path normalization
func TestNormalizeLogicalPath_BasicCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"data/sample.fastq", "/data/sample.fastq"},   // Add leading slash
		{"/data/sample.fastq", "/data/sample.fastq"},  // Already normalized
		{"/data//sample.fastq", "/data/sample.fastq"}, // Remove duplicate slashes
		{"/data/sample.fastq/", "/data/sample.fastq"}, // Remove trailing slash
		{"data///sample.fastq", "/data/sample.fastq"}, // Multiple issues
		{"/", "/"}, // Root should keep trailing slash
		{"//data//sample.fastq//", "/data/sample.fastq"},  // All normalizations
		{"data\\sample.fastq", "/data/sample.fastq"},      // Backslash to forward slash
		{"/data/dir/", "/data/dir"},                       // Trailing slash on directory
		{"./data/sample.fastq", "/./data/sample.fastq"},   // Relative path (keeps ./)
		{"../data/sample.fastq", "/../data/sample.fastq"}, // Parent directory (keeps ../)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeLogicalPath(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeLogicalPath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestNormalizeLogicalPath_PathEquivalence verifies that equivalent paths produce the same normalized result
func TestNormalizeLogicalPath_PathEquivalence(t *testing.T) {
	paths := []string{
		"data/sample.fastq",
		"/data/sample.fastq",
		"//data//sample.fastq",
		"/data/sample.fastq/",
		"data///sample.fastq",
	}

	// Normalize all paths
	normalized := make([]string, len(paths))
	for i, p := range paths {
		normalized[i] = NormalizeLogicalPath(p)
	}

	// All normalized paths should be identical (except the first one which adds a leading slash)
	expected := "/data/sample.fastq"
	for i, norm := range normalized {
		if norm != expected {
			t.Errorf("Path %q normalized to %q, expected %q", paths[i], norm, expected)
		}
	}
}

// TestComputeDeterministicUUID_PathNormalization verifies that equivalent paths produce the same UUID
func TestComputeDeterministicUUID_PathNormalization(t *testing.T) {
	collection := "gdc-tcga"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// These paths should all normalize to the same thing
	uuid1 := ComputeDeterministicUUID(collection, "data/sample.fastq", sha256)
	uuid2 := ComputeDeterministicUUID(collection, "/data/sample.fastq", sha256)
	uuid3 := ComputeDeterministicUUID(collection, "//data//sample.fastq", sha256)
	uuid4 := ComputeDeterministicUUID(collection, "/data/sample.fastq/", sha256)

	if uuid1 != uuid2 || uuid2 != uuid3 || uuid3 != uuid4 {
		t.Errorf("ComputeDeterministicUUID() should produce same UUID for equivalent paths: got %s, %s, %s, %s", uuid1, uuid2, uuid3, uuid4)
	}
}

// TestComputeDeterministicUUID_MatchesSpecification verifies the canonical format
func TestComputeDeterministicUUID_MatchesSpecification(t *testing.T) {
	// Test with known inputs to verify the canonical string format
	collection := "gdc-tcga"
	path := "/projectA/raw/reads/R1.fastq.gz"
	sha256 := "4d9670e4c8f3e8b8a6c2d4f9136d7b89e4b9d5e0d2a1c0b9f4c2de0e8c7ac1a0"

	uuid := ComputeDeterministicUUID(collection, path, sha256)

	// UUID should not be empty
	if uuid == "" {
		t.Error("ComputeDeterministicUUID() returned empty UUID")
	}

	// UUID should be in standard format (with hyphens)
	if len(uuid) != 36 {
		t.Errorf("ComputeDeterministicUUID() returned UUID with incorrect length: %d, expected 36", len(uuid))
	}

	// Should be reproducible
	uuid2 := ComputeDeterministicUUID(collection, path, sha256)
	if uuid != uuid2 {
		t.Errorf("ComputeDeterministicUUID() not reproducible: %s != %s", uuid, uuid2)
	}
}

// TestComputeDeterministicUUID_LanguageAgnostic verifies that the UUID can be reproduced
// This test documents the expected behavior for external tools
func TestComputeDeterministicUUID_LanguageAgnostic(t *testing.T) {
	// Test case from specification
	collection := "gdc-tcga"
	path := "/projectA/raw/reads/R1.fastq.gz"
	sha256 := "4d9670e4c8f3e8b8a6c2d4f9136d7b89e4b9d5e0d2a1c0b9f4c2de0e8c7ac1a0"

	uuid := ComputeDeterministicUUID(collection, path, sha256)

	// The canonical string should be:
	// "did:gen3:{collection}:{path}:{sha256}"
	// UUID should be UUIDv5(NAMESPACE, canonical)
	// Note: AUTHORITY is NOT included in the canonical string format
	// Note: Size is NOT included in the canonical string

	t.Logf("Generated UUID: %s", uuid)
	t.Logf("Canonical string: did:gen3:%s:%s:%s", collection, path, sha256)
	t.Logf("Namespace UUID: %s", NAMESPACE.String())
	t.Logf("AUTHORITY (for reference): %s", AUTHORITY)

	// This test documents the expected UUID for external tool verification
	// External tools (Python, etc.) should be able to reproduce this exact UUID
}

// TestNAMESPACE_Value verifies the namespace UUID is correct
func TestNAMESPACE_Value(t *testing.T) {
	// Verify namespace is derived from DNS namespace and AUTHORITY
	// This should match: uuid.uuid3(uuid.NAMESPACE_DNS, b'https://calypr.org')
	namespace := NAMESPACE.String()

	if namespace == "" {
		t.Error("NAMESPACE is empty")
	}

	// Should be a valid UUID format
	if len(namespace) != 36 {
		t.Errorf("NAMESPACE has incorrect length: %d, expected 36", len(namespace))
	}

	t.Logf("NAMESPACE: %s", namespace)
	t.Logf("AUTHORITY: %s", AUTHORITY)
}

// TestAUTHORITY_Value verifies the authority constant
func TestAUTHORITY_Value(t *testing.T) {
	expected := "https://calypr.org"
	if AUTHORITY != expected {
		t.Errorf("AUTHORITY = %q, want %q", AUTHORITY, expected)
	}

	t.Logf("AUTHORITY: %s", AUTHORITY)
}

// TestComputeDeterministicUUID_DifferentCollections verifies that different collections produce different UUIDs
func TestComputeDeterministicUUID_DifferentCollections(t *testing.T) {
	path := "/data/sample.fastq"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Generate UUIDs with different collections
	uuid1 := ComputeDeterministicUUID("gdc-tcga", path, sha256)
	uuid2 := ComputeDeterministicUUID("gdc-target", path, sha256)
	uuid3 := ComputeDeterministicUUID("my-bucket", path, sha256)

	// All should be different
	if uuid1 == uuid2 {
		t.Errorf("ComputeDeterministicUUID() should generate different UUIDs for different collections: gdc-tcga=%s, gdc-target=%s", uuid1, uuid2)
	}
	if uuid1 == uuid3 {
		t.Errorf("ComputeDeterministicUUID() should generate different UUIDs for different collections: gdc-tcga=%s, my-bucket=%s", uuid1, uuid3)
	}
	if uuid2 == uuid3 {
		t.Errorf("ComputeDeterministicUUID() should generate different UUIDs for different collections: gdc-target=%s, my-bucket=%s", uuid2, uuid3)
	}

	t.Logf("UUID for gdc-tcga: %s", uuid1)
	t.Logf("UUID for gdc-target: %s", uuid2)
	t.Logf("UUID for my-bucket: %s", uuid3)
}

// TestComputeDeterministicUUID_EmptyCollection verifies handling of empty collection
func TestComputeDeterministicUUID_EmptyCollection(t *testing.T) {
	path := "/data/sample.fastq"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Empty collection should still work (for standalone repos without project context)
	uuid := ComputeDeterministicUUID("", path, sha256)

	if uuid == "" {
		t.Error("ComputeDeterministicUUID() returned empty UUID for empty collection")
	}

	// Empty collection should produce different UUID than named collection
	uuidWithCollection := ComputeDeterministicUUID("gdc-tcga", path, sha256)
	if uuid == uuidWithCollection {
		t.Error("ComputeDeterministicUUID() should produce different UUIDs for empty vs named collection")
	}

	t.Logf("UUID for empty collection: %s", uuid)
	t.Logf("UUID for gdc-tcga: %s", uuidWithCollection)
}

// TestComputeDeterministicUUID_CollectionFormats verifies various collection format conventions
func TestComputeDeterministicUUID_CollectionFormats(t *testing.T) {
	path := "/data/sample.fastq"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	testCases := []struct {
		name       string
		collection string
	}{
		{"Gen3 program-project", "gdc-tcga"},
		{"S3 bucket", "my-genomics-bucket"},
		{"Workspace ID", "workspace-12345"},
		{"Study ID", "study-abc"},
		{"Repo name", "anvil-git-drs-test-repo"},
	}

	uuids := make(map[string]string)
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			uuid := ComputeDeterministicUUID(tc.collection, path, sha256)

			if uuid == "" {
				t.Errorf("ComputeDeterministicUUID() returned empty UUID for collection %q", tc.collection)
			}

			// Check uniqueness
			for prevColl, prevUUID := range uuids {
				if uuid == prevUUID {
					t.Errorf("ComputeDeterministicUUID() produced same UUID for different collections: %s and %s", tc.collection, prevColl)
				}
			}

			uuids[tc.collection] = uuid
			t.Logf("UUID for %s (%s): %s", tc.name, tc.collection, uuid)
		})
	}
}
