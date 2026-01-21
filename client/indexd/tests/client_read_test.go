package indexd_tests

import (
	"testing"

	"github.com/calypr/git-drs/drs/hash"
	"github.com/stretchr/testify/require"
)

///////////////////
// READ TESTS    //
///////////////////

// Integration tests for READ operations on IndexdClient using mock indexd server.
// These tests verify non-mutating operations that query and retrieve data:
// - GetRecord / GetIndexdRecordByDID - Retrieve a single record by DID
// - GetObjectsByHash - Query records by hash
// - GetDownloadURL - Get signed download URLs
// - GetProjectId - Simple getter for project ID

// TestIndexdClient_GetRecord tests retrieving a record via the client method with mocked auth
func TestIndexdClient_GetRecord(t *testing.T) {
	// Arrange: Start mock server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Pre-populate mock with test record
	testRecord := newTestRecord("uuid-test-123")
	addRecordToMockServer(mockServer, testRecord)

	// Act: Use client method with mocked auth (tests actual client logic)
	client := testIndexdClientWithMockAuth(mockServer.URL())
	record, err := client.GetIndexdRecordByDID(testRecord.Did)

	// Assert: Test actual client logic
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, testRecord.Did, record.Did)
	require.Equal(t, testRecord.Size, record.Size)
	require.Equal(t, testRecord.FileName, record.FileName)
}

// TestIndexdClient_GetRecord_NotFound tests error handling for non-existent records
func TestIndexdClient_GetRecord_NotFound(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Act: Use client method to request non-existent record
	client := testIndexdClientWithMockAuth(mockServer.URL())
	record, err := client.GetIndexdRecordByDID("does-not-exist")

	// Assert: Client should handle 404 errors properly
	require.Error(t, err)
	require.Nil(t, record)
	require.Contains(t, err.Error(), "failed to get record")
}

///////////////////////////////
// GetObjectsByHash Tests
///////////////////////////////

// TestIndexdClient_GetObjectsByHash tests hash-based queries via client method with mocked auth
func TestIndexdClient_GetObjectsByHash(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	testRecord := newTestRecord("uuid-test-456", withTestRecordSize(2048))
	sha256 := testRecord.Hashes["sha256"]
	addRecordWithHashIndex(mockServer, testRecord, "sha256", sha256)

	// Create client with mocked auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act: Call the actual client method
	results, err := client.GetObjectByHash(&hash.Checksum{Type: "sha256", Checksum: sha256})

	// Assert: Verify client method works end-to-end
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify correct record was returned
	record := results[0]
	require.Equal(t, testRecord.Did, record.Id)
	require.Equal(t, testRecord.Size, record.Size)
	require.Equal(t, sha256, record.Checksums.SHA256)

	require.Equal(t, testRecord.URLs[0], record.AccessMethods[0].AccessURL.URL)
	require.Equal(t, testRecord.Authz[0], record.AccessMethods[0].Authorizations.Value)

	// Test: Query with non-existent hash
	emptyResults, err := client.GetObjectByHash(&hash.Checksum{Type: "sha256", Checksum: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	require.NoError(t, err)
	require.Len(t, emptyResults, 0)
}

///////////////////////////////
// GetProjectId Tests
///////////////////////////////

// TestIndexdClient_GetProjectId tests the simple getter for project ID
func TestIndexdClient_GetProjectId(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act
	projectId := client.GetProjectId()

	// Assert: Should return the project ID set during client creation
	require.Equal(t, "test-project", projectId, "Should return configured project ID")
}

// TestIndexdClient_GetProjectId_ConsistentAcrossCalls tests that GetProjectId is consistent
func TestIndexdClient_GetProjectId_ConsistentAcrossCalls(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act: Call multiple times
	projectId1 := client.GetProjectId()
	projectId2 := client.GetProjectId()
	projectId3 := client.GetProjectId()

	// Assert: All calls should return the same value
	require.Equal(t, projectId1, projectId2, "GetProjectId should be consistent")
	require.Equal(t, projectId2, projectId3, "GetProjectId should be consistent")
	require.Equal(t, "test-project", projectId1)
}
