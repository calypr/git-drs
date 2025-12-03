package indexd_tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"log"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/s3_utils"
	"github.com/stretchr/testify/require"
)

// TestIndexdClient_GetDownloadURL tests the complete GetDownloadURL flow with various scenarios.
// This test covers the main success paths and error conditions, while panic scenarios
// are tested separately in TestIndexdClient_GetDownloadURL_PanicScenarios.
//
// The method flow is:
// 1. GetObjectsByHash() - Query indexd for records matching the OID hash
// 2. FindMatchingRecord() - Find a record matching the client's project ID
// 3. GetObject() - Get the DRS object using the matching record's DID
// 4. Extract AccessID from AccessMethods[0]
// 5. HTTP GET to /ga4gh/drs/v1/objects/{id}/access/{accessId} for signed URL
//
// Note: FindMatchingRecord is already tested in add-url-unit_test.go, so we use
// real data that matches the expected authorization format.
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

// Test edge cases and error handling scenarios separately
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

// testErrorAuthHandler is a test helper for testing auth errors
type testErrorAuthHandler struct {
	err error
}

func (e *testErrorAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	return e.err
}

// Helper function to parse URL
func parseURL(rawURL string) *url.URL {
	u, _ := url.Parse(rawURL)
	return u
}
