package client

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/utils"
	"github.com/stretchr/testify/require"
)

//////////////////
// TEST HELPERS //
//////////////////

// buildTestConfig creates an in-memory config for testing
func buildTestConfig(gen3URL, indexdURL, s3URL string) *config.Config {
	return &config.Config{
		CurrentServer: config.Gen3ServerType,
		Servers: config.ServersMap{
			Gen3: &config.Gen3Server{
				Endpoint: gen3URL,
				Auth: config.Gen3Auth{
					Profile:   "test-profile",
					ProjectID: "test-project",
					Bucket:    "test-bucket",
				},
			},
		},
	}
}

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

// TestMockIndexdServer_ThreadSafety tests concurrent access
func TestMockIndexdServer_ThreadSafety(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Add initial record
	initialRecord := &MockIndexdRecord{
		Did:    "uuid-initial",
		Size:   1024,
		URLs:   []string{"s3://bucket/file.bam"},
		Hashes: map[string]string{"sha256": "aaaa..."},
	}

	mockServer.recordMutex.Lock()
	mockServer.records[initialRecord.Did] = initialRecord
	mockServer.hashIndex["sha256:aaaa..."] = []string{initialRecord.Did}
	mockServer.recordMutex.Unlock()

	// Concurrent reads should be safe
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			client := testIndexdClient(mockServer.URL())
			_, _ = client.getIndexdRecordByDID("uuid-initial")
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify record still exists
	record := mockServer.GetRecord("uuid-initial")
	require.NotNil(t, record)
}

// TestMockServers_AllEndpoints tests all mock server endpoints
func TestMockServers_AllEndpoints(t *testing.T) {
	// Test Indexd endpoints
	indexdMock := NewMockIndexdServer(t)
	defer indexdMock.Close()

	client := testIndexdClient(indexdMock.URL())
	require.NotNil(t, client)

	// Test Gen3 endpoint
	gen3Mock := NewMockGen3Server(t, "http://localhost:9000")
	defer gen3Mock.Close()

	resp, err := http.Get(gen3Mock.URL() + "/user/data/buckets")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var bucketResponse map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&bucketResponse)
	require.NoError(t, err)
	require.NotNil(t, bucketResponse["S3_BUCKETS"])

	// Test S3 endpoint
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	s3Mock.AddObject("test-bucket", "test.bam", 1024)

	resp, err = http.Head(s3Mock.URL() + "/test-bucket/test.bam")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "1024", resp.Header.Get("Content-Length"))
}
