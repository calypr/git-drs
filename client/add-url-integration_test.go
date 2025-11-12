package client

import (
	"testing"

	"github.com/calypr/git-drs/utils"
	"github.com/stretchr/testify/require"
)

////////////////////
// E2E TESTS      //
// & MISC TESTS   //
////////////////////

// TestAddURL_E2E_IdempotentSameURL tests end-to-end idempotency
func TestAddURL_E2E_IdempotentSameURL(t *testing.T) {
	// Arrange: Start mock servers
	gen3Mock := NewMockGen3Server(t, "http://localhost:9000")
	defer gen3Mock.Close()

	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	indexdMock := NewMockIndexdServer(t)
	defer indexdMock.Close()

	// Pre-populate S3 with test object
	s3Mock.AddObject("test-bucket", "sample.bam", 1024)

	// TODO: This test is limited because AddURL has hardcoded config.LoadConfig()
	// In a real scenario, we'd need to mock that too or refactor AddURL to accept config
	t.Skip("Requires AddURL refactoring to accept config parameter")
}

// TestAddURL_E2E_UpdateDifferentURL tests updating record with different URL
// TODO: stubbed
func TestAddURL_E2E_UpdateDifferentURL(t *testing.T) {
	// TODO: This test is skipped because it requires AddURL refactoring
	// See TestAddURL_E2E_IdempotentSameURL for explanation
	t.Skip("Requires AddURL refactoring to accept config parameter")
}

// TestAddURL_E2E_LFSNotTracked tests LFS validation
func TestAddURL_E2E_LFSNotTracked(t *testing.T) {
	// This test validates the LFS tracking check
	// The actual utils.IsLFSTracked function is tested separately in utils package

	// Test the pattern matching logic by verifying ParseGitAttributes works
	gitattributesContent := `*.bam filter=lfs diff=lfs merge=lfs -text
*.vcf filter=lfs diff=lfs merge=lfs -text`

	attributes, err := utils.ParseGitAttributes(gitattributesContent)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(attributes), 2)

	// Verify .bam pattern exists
	found := false
	for _, attr := range attributes {
		if attr.Pattern == "*.bam" {
			if filter, exists := attr.Attributes["filter"]; exists {
				require.Equal(t, "lfs", filter)
				found = true
			}
		}
	}
	require.True(t, found, "*.bam pattern with lfs filter should exist")
}
