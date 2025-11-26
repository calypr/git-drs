package indexd_tests

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/log"
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
		setupMockData     func(*MockIndexdServer)
		expectError       string
		validateAccessURL func(*testing.T, *drs.AccessURL)
	}{
		{
			name: "successful download URL retrieval",
			oid:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			setupMockData: func(server *MockIndexdServer) {
				// Add a record that matches the test project
				record := &MockIndexdRecord{
					Did:      "test-did-123",
					FileName: "test.bam",
					Size:     1024,
					Hashes:   map[string]string{"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
					URLs:     []string{"s3://test-bucket/test.bam"},
					Authz:    []string{"/programs/test/projects/project"},
				}

				server.recordMutex.Lock()
				server.records[record.Did] = record
				hashKey := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
				server.hashIndex[hashKey] = []string{record.Did}
				server.recordMutex.Unlock()
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				// The mock server creates signed URLs in the format: https://signed-url.example.com/{did}/{accessId}
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/test-did-123/")
			},
		},
		{
			name: "no records found for hash",
			oid:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			setupMockData: func(server *MockIndexdServer) {
				// No records added - will return empty response
			},
			expectError: "no DRS object found for OID bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name: "successful download with matching project",
			oid:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			setupMockData: func(server *MockIndexdServer) {
				// Add record with matching project authorization
				record := &MockIndexdRecord{
					Did:      "test-did-456",
					FileName: "matching.bam",
					Size:     2048,
					Hashes:   map[string]string{"sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
					URLs:     []string{"s3://test-bucket/matching.bam"},
					Authz:    []string{"/programs/test/projects/project"}, // Same project as client
				}

				server.recordMutex.Lock()
				server.records[record.Did] = record
				hashKey := "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
				server.hashIndex[hashKey] = []string{record.Did}
				server.recordMutex.Unlock()
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/test-did-456/")
			},
		},
		{
			name: "successful second download URL with different hash",
			oid:  "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			setupMockData: func(server *MockIndexdServer) {
				// Add another valid record with different hash
				record := &MockIndexdRecord{
					Did:      "test-did-789",
					FileName: "second.bam",
					Size:     512,
					Hashes:   map[string]string{"sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
					URLs:     []string{"s3://test-bucket/second.bam"},
					Authz:    []string{"/programs/test/projects/project"},
				}

				server.recordMutex.Lock()
				server.records[record.Did] = record
				hashKey := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
				server.hashIndex[hashKey] = []string{record.Did}
				server.recordMutex.Unlock()
			},
			validateAccessURL: func(t *testing.T, accessURL *drs.AccessURL) {
				require.NotNil(t, accessURL)
				require.NotEmpty(t, accessURL.URL)
				require.Contains(t, accessURL.URL, "https://signed-url.example.com/test-did-789/")
			},
		},
		{
			name: "auth handler returns error",
			oid:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			setupMockData: func(server *MockIndexdServer) {
				record := &MockIndexdRecord{
					Did:      "test-did-auth-error",
					FileName: "auth-error.bam",
					Size:     1024,
					Hashes:   map[string]string{"sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
					URLs:     []string{"s3://test-bucket/auth-error.bam"},
					Authz:    []string{"/programs/test/projects/project"},
				}

				server.recordMutex.Lock()
				server.records[record.Did] = record
				hashKey := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
				server.hashIndex[hashKey] = []string{record.Did}
				server.recordMutex.Unlock()
			},
			expectError: "error getting DRS object for OID eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock server
			mockServer := NewMockIndexdServer(t)
			defer mockServer.Close()
			tt.setupMockData(mockServer)

			// Create client with appropriate auth handler
			var authHandler s3_utils.AuthHandler = &MockAuthHandler{}
			if tt.name == "auth handler returns error" {
				authHandler = &testErrorAuthHandler{err: fmt.Errorf("auth failed")}
			}

			client := &indexd_client.IndexDClient{
				Base:        parseURL(mockServer.URL()),
				Profile:     "test-profile",
				ProjectId:   "test-project", // This will become /programs/test/projects/project
				BucketName:  "test-bucket",
				Logger:      &log.NoOpLogger{},
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
					tt.validateAccessURL(t, result)
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
		record := &MockIndexdRecord{
			Did:      "test-did-different-project",
			FileName: "other.bam",
			Size:     2048,
			Hashes:   map[string]string{"sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},
			URLs:     []string{"s3://other-bucket/other.bam"},
			Authz:    []string{"/programs/other/projects/other-project"}, // Different project
		}

		mockServer.recordMutex.Lock()
		mockServer.records[record.Did] = record
		hashKey := "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		mockServer.hashIndex[hashKey] = []string{record.Did}
		mockServer.recordMutex.Unlock()

		client := testIndexdClientWithMockAuth(mockServer.URL())

		// This should return error
		result, err := client.GetDownloadURL("dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")

		// Verify proper error handling
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "no matching record found for project")
	})

	t.Run("no access methods returns proper error", func(t *testing.T) {
		mockServer := NewMockIndexdServer(t)
		defer mockServer.Close()

		// Add record with no URLs (which creates DRS object with no access methods)
		record := &MockIndexdRecord{
			Did:      "test-did-no-access",
			FileName: "no-access.bam",
			Size:     512,
			Hashes:   map[string]string{"sha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
			URLs:     []string{}, // Empty URLs means no access methods
			Authz:    []string{"/programs/test/projects/project"},
		}

		mockServer.recordMutex.Lock()
		mockServer.records[record.Did] = record
		hashKey := "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		mockServer.hashIndex[hashKey] = []string{record.Did}
		mockServer.recordMutex.Unlock()

		client := testIndexdClientWithMockAuth(mockServer.URL())

		// This should return an error
		result, err := client.GetDownloadURL("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")

		// Verify proper error handling
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "no access methods available for DRS object")
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
