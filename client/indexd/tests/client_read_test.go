package indexd_tests

import (
	"fmt"
	"log"
	"testing"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/s3_utils"
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
	results, err := client.GetObjectsByHash(&drs.Checksum{Type: "sha256", Checksum: sha256})

	// Assert: Verify client method works end-to-end
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify correct record was returned
	record := results[0]
	require.Equal(t, testRecord.Did, record[0].Id)
	require.Equal(t, testRecord.Size, record[0].Size)
	require.Equal(t, sha256, record[0].Checksums[0].Checksum)
	require.Equal(t, drs.ChecksumTypeSHA256, record[0].Checksums[0].Type)

	require.Equal(t, testRecord.URLs[0], record[0].AccessMethods[0].AccessURL.URL)
	require.Equal(t, testRecord.Authz[0], record[0].AccessMethods[0].Authorizations.Value)

	// Test: Query with non-existent hash
	emptyResults, err := client.GetObjectsByHash(&drs.Checksum{Type: "sha256", Checksum: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	require.NoError(t, err)
	require.Len(t, emptyResults, 0)
}

///////////////////////////////
// GetDownloadURL Tests
///////////////////////////////

// TestIndexdClient_GetDownloadURL tests the complete flow of getting a signed download URL
// Flow: GetObjectsByHash -> FindMatchingRecord -> GetObject -> Extract AccessID -> Get Signed URL
func TestIndexdClient_GetDownloadURL(t *testing.T) {
	tests := []struct {
		name              string
		oid               string
		setupMockData     func(*MockIndexdServer) *MockIndexdRecord // Return record for validation
		expectError       string
		validateAccessURL func(*testing.T, *drs.AccessURL, *MockIndexdRecord)
	}{
		{
			name: "successful download URL retrieval",
			oid:  testSHA256Hash,
			setupMockData: func(server *MockIndexdServer) *MockIndexdRecord {
				// Add a record that matches the test project
				record := newTestRecord("test-did-123")
				addRecordWithHashIndex(server, record, "sha256", testSHA256Hash)
				return record
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL, record *MockIndexdRecord) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				// The mock server creates signed URLs in the format: https://signed-url.example.com/{did}/{accessId}
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/"+record.Did+"/")
			},
		},
		{
			name: "no records found for hash",
			oid:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			setupMockData: func(server *MockIndexdServer) *MockIndexdRecord {
				// No records added - will return empty response
				return nil
			},
			expectError: "no DRS object found for OID bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name: "successful download with matching project",
			oid:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			setupMockData: func(server *MockIndexdServer) *MockIndexdRecord {
				// Add record with matching project authorization
				record := newTestRecord("test-did-456",
					withTestRecordFileName("matching.bam"),
					withTestRecordSize(2048),
					withTestRecordHash("sha256", "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"))
				addRecordWithHashIndex(server, record, "sha256", record.Hashes["sha256"])
				return record
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL, record *MockIndexdRecord) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/"+record.Did+"/")
			},
		},
		{
			name: "successful second download URL with different hash",
			oid:  "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			setupMockData: func(server *MockIndexdServer) *MockIndexdRecord {
				// Add another valid record with different hash
				record := newTestRecord("test-did-789",
					withTestRecordFileName("second.bam"),
					withTestRecordSize(512),
					withTestRecordHash("sha256", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"))
				addRecordWithHashIndex(server, record, "sha256", record.Hashes["sha256"])
				return record
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL, record *MockIndexdRecord) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/"+record.Did+"/")
			},
		},
		{
			name: "auth handler returns error",
			oid:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			setupMockData: func(server *MockIndexdServer) *MockIndexdRecord {
				record := newTestRecord("test-did-auth-error",
					withTestRecordFileName("auth-error.bam"),
					withTestRecordHash("sha256", "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"))
				addRecordWithHashIndex(server, record, "sha256", record.Hashes["sha256"])
				return record
			},
			expectError: "error getting DRS object for OID eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock server
			mockServer := NewMockIndexdServer(t)
			defer mockServer.Close()
			record := tt.setupMockData(mockServer)

			// Create client with appropriate auth handler
			var authHandler s3_utils.AuthHandler = &MockAuthHandler{}
			if tt.name == "auth handler returns error" {
				authHandler = &testErrorAuthHandler{err: fmt.Errorf("auth failed")}
			}

			client := &indexd_client.IndexDClient{
				Base:        parseURL(mockServer.URL()),
				Remote:      "test-remote",
				ProjectId:   "test-project", // This will become /programs/test/projects/project
				BucketName:  "test-bucket",
				Logger:      log.Default(),
				AuthHandler: authHandler,
			}

			// Execute method under test
			result, err := client.GetDownloadURL(tt.oid)

			// Validate results
			if tt.expectError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectError)
				require.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tt.validateAccessURL != nil {
					tt.validateAccessURL(t, result, record)
				}
			}
		})
	}
}

// TestIndexdClient_GetDownloadURL_EdgeCases tests edge cases and error handling scenarios
func TestIndexdClient_GetDownloadURL_EdgeCases(t *testing.T) {
	t.Run("empty OID should still make request", func(t *testing.T) {
		mockServer := NewMockIndexdServer(t)
		defer mockServer.Close()

		client := testIndexdClientWithMockAuth(mockServer.URL())

		result, err := client.GetDownloadURL("")

		// Should fail because no records exist for empty hash
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "no DRS object found for OID")
	})

	t.Run("OID with special characters causes URL parse error", func(t *testing.T) {
		mockServer := NewMockIndexdServer(t)
		defer mockServer.Close()

		client := testIndexdClientWithMockAuth(mockServer.URL())
		specialOid := "test-hash-with-special-chars!@#$%"

		result, err := client.GetDownloadURL(specialOid)

		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "invalid URL escape")
	})
}

// TestIndexdClient_GetDownloadURL_PanicScenarios tests error conditions that previously caused panics
// These tests verify that the bugs have been fixed and now return proper errors instead of panicking.
func TestIndexdClient_GetDownloadURL_PanicScenarios(t *testing.T) {
	t.Run("no matching record returns proper error", func(t *testing.T) {
		mockServer := NewMockIndexdServer(t)
		defer mockServer.Close()

		// Add record with different project authorization that won't match
		differentProjectAuthz := "/programs/other/projects/other-project"
		testHash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		record := newTestRecord("test-did-different-project",
			withTestRecordFileName("other.bam"),
			withTestRecordSize(2048),
			withTestRecordURLs("s3://other-bucket/other.bam"),
			withTestRecordHash("sha256", testHash))
		record.Authz = []string{differentProjectAuthz} // Override with different project
		addRecordWithHashIndex(mockServer, record, "sha256", testHash)

		client := testIndexdClientWithMockAuth(mockServer.URL())

		// This should return error
		result, err := client.GetDownloadURL(testHash)

		// Verify proper error handling
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "no matching record found for project")
	})

	t.Run("no access methods returns proper error", func(t *testing.T) {
		mockServer := NewMockIndexdServer(t)
		defer mockServer.Close()

		// Add record with no URLs (which creates DRS object with no access methods)
		// Note: A record with no URLs can't be matched by project because FindMatchingRecord
		// requires access methods with authorizations. So this will fail at the matching stage.
		testHash := "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		record := newTestRecord("test-did-no-access",
			withTestRecordFileName("no-access.bam"),
			withTestRecordSize(512),
			withTestRecordURLs(), // Empty URLs means no access methods
			withTestRecordHash("sha256", testHash))
		addRecordWithHashIndex(mockServer, record, "sha256", testHash)

		client := testIndexdClientWithMockAuth(mockServer.URL())

		// This should return an error about no matching record
		// (because records without access methods can't be matched by project)
		result, err := client.GetDownloadURL(testHash)

		// Verify proper error handling
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "no matching record found for project")
	})
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
