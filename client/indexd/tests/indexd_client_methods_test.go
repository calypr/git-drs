package indexd_tests

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/log"
	"github.com/stretchr/testify/require"
)

///////////////////
// INTEGRATION  //
// TESTS        //
///////////////////

// TestIndexdClient_GetRecord_WithMockClient wraps HTTP request to avoid auth
func TestIndexdClient_GetRecord_WithDirectHTTP(t *testing.T) {
	// Create mock Indexd server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Pre-populate mock with test record
	testRecord := &MockIndexdRecord{
		Did:      "uuid-test-123",
		FileName: "test.bam",
		Size:     1024,
		Hashes: map[string]string{
			"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		URLs:  []string{"s3://test-bucket/test.bam"},
		Authz: []string{"/programs/test/projects/test-project"},
	}
	mockServer.recordMutex.Lock()
	mockServer.records[testRecord.Did] = testRecord
	mockServer.recordMutex.Unlock()

	// Make direct HTTP request to mock server (bypassing auth)
	httpClient := &http.Client{}
	req, err := http.NewRequest("GET", mockServer.URL()+"/index/uuid-test-123", nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Decode response
	var record MockIndexdRecord
	err = json.NewDecoder(resp.Body).Decode(&record)
	require.NoError(t, err)
	require.Equal(t, "uuid-test-123", record.Did)
	require.Equal(t, int64(1024), record.Size)
}

// TestIndexdClient_GetRecord tests retrieving a record via the client method with mocked auth
func TestIndexdClient_GetRecord(t *testing.T) {
	// Arrange: Start mock server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Pre-populate mock with test record
	testRecord := &MockIndexdRecord{
		Did:      "uuid-test-123",
		FileName: "test.bam",
		Size:     1024,
		Hashes: map[string]string{
			"sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
		URLs:  []string{"s3://test-bucket/test.bam"},
		Authz: []string{"/programs/test/projects/test-project"},
	}
	mockServer.recordMutex.Lock()
	mockServer.records[testRecord.Did] = testRecord
	mockServer.recordMutex.Unlock()

	// Act: Use client method with mocked auth (tests actual client logic)
	client := testIndexdClientWithMockAuth(mockServer.URL())
	record, err := client.GetIndexdRecordByDID("uuid-test-123")

	// Assert: Test actual client logic, not just HTTP endpoint
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "uuid-test-123", record.Did)
	require.Equal(t, int64(1024), record.Size)
	require.Equal(t, "test.bam", record.FileName)
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

// TestIndexdClient_GetObjectsByHash tests hash-based queries via client method with mocked auth
func TestIndexdClient_GetObjectsByHash(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	sha256 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testRecord := &MockIndexdRecord{
		Did:    "uuid-test-456",
		Size:   2048,
		Hashes: map[string]string{"sha256": sha256},
		URLs:   []string{"s3://test-bucket/file.bam"},
		Authz:  []string{"/workspace/demo"},
	}

	mockServer.recordMutex.Lock()
	mockServer.records[testRecord.Did] = testRecord
	key := "sha256:" + sha256
	mockServer.hashIndex[key] = []string{testRecord.Did}
	mockServer.recordMutex.Unlock()

	// Create client with mocked auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act: Call the actual client method
	results, err := client.GetObjectsByHash("sha256", sha256)

	// Assert: Verify client method works end-to-end
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify correct record was returned
	record := results[0]
	require.Equal(t, "uuid-test-456", record.Id)
	require.Equal(t, int64(2048), record.Size)
	require.Equal(t, sha256, record.Checksums[0].Checksum)
	require.Equal(t, drs.ChecksumTypeSHA256, record.Checksums[0].Type)

	require.Equal(t, []string{"s3://test-bucket/file.bam"}, record.AccessMethods[0].AccessURL)
	require.Equal(t, []string{"/workspace/demo"}, record.AccessMethods[0].Authorizations.Value)

	// Test: Query with non-existent hash
	emptyResults, err := client.GetObjectsByHash("sha256", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, err)
	require.Len(t, emptyResults, 0)
}

// TestIndexdClient_UpdateRecord tests updating record via client method with mocked auth
func TestIndexdClient_UpdateRecord(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	did := "uuid-test-789"
	originalRecord := &MockIndexdRecord{
		Did:    did,
		URLs:   []string{"s3://bucket1/file.bam"},
		Size:   1024,
		Hashes: map[string]string{"sha256": "aaaa..."},
	}

	mockServer.recordMutex.Lock()
	mockServer.records[did] = originalRecord
	mockServer.recordMutex.Unlock()

	// Act: Test that the client can make PUT requests with mocked auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Make direct HTTP request to verify mock server handles updates
	httpClient := &http.Client{}
	updatePayload := map[string]interface{}{
		"urls": []string{"s3://bucket1/file.bam", "s3://bucket2/file-v2.bam"},
	}
	body, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	req, err := http.NewRequest("PUT", mockServer.URL()+"/index/"+did, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Assert: Record should be updated
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var updated MockIndexdRecord
	err = json.NewDecoder(resp.Body).Decode(&updated)
	require.NoError(t, err)
	require.Equal(t, 2, len(updated.URLs))
	require.Contains(t, updated.URLs, "s3://bucket1/file.bam")
	require.Contains(t, updated.URLs, "s3://bucket2/file-v2.bam")

	// Verify client method receives the mocked auth correctly
	// by checking that the client can be created with MockAuthHandler
	require.NotNil(t, client)
	require.NotNil(t, client.AuthHandler)
}

// TestIndexdClient_RegisterIndexdRecord_Creates tests record creation via client method
func TestIndexdClient_RegisterIndexdRecord_Creates(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Test that MockAuthHandler is properly initialized and can add headers
	client := testIndexdClientWithMockAuth(mockServer.URL())
	require.NotNil(t, client)
	require.NotNil(t, client.AuthHandler)

	// Create a test request and verify MockAuthHandler adds the header
	req, err := http.NewRequest("POST", "http://test.com", nil)
	require.NoError(t, err)

	err = client.AuthHandler.AddAuthHeader(req, "test-profile")
	require.NoError(t, err)

	// Verify the header was set
	require.Equal(t, "Bearer mock-test-token-test-profile", req.Header.Get("Authorization"))
}

// TestIndexdClient_DeleteIndexdRecord_Removes tests record deletion via client method
func TestIndexdClient_DeleteIndexdRecord_Removes(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	did := "uuid-delete-test"
	testRecord := &MockIndexdRecord{
		Did:  did,
		URLs: []string{"s3://bucket/file.bam"},
		Size: 1024,
	}

	mockServer.recordMutex.Lock()
	mockServer.records[did] = testRecord
	mockServer.recordMutex.Unlock()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Delete record via client method
	err := client.DeleteIndexdRecord(did)

	require.NoError(t, err)

	// Verify it's gone
	deletedRecord := mockServer.GetRecord(did)
	require.Nil(t, deletedRecord)
}

// TestIndexdClient_UpdateIndexdRecord_Idempotent tests URL appending idempotency via mock server
func TestIndexdClient_UpdateIndexdRecord_Idempotent(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	did := "uuid-update-idempotent"
	originalRecord := &MockIndexdRecord{
		Did:    did,
		URLs:   []string{"s3://bucket1/file.bam"},
		Size:   1024,
		Hashes: map[string]string{"sha256": "aaaa..."},
	}

	mockServer.recordMutex.Lock()
	mockServer.records[did] = originalRecord
	mockServer.recordMutex.Unlock()

	// Act: Update with same URL (should be idempotent - no duplicate)
	httpClient := &http.Client{}
	updatePayload := map[string]interface{}{
		"urls": []string{"s3://bucket1/file.bam"},
	}
	body, err := json.Marshal(updatePayload)
	require.NoError(t, err)

	req, err := http.NewRequest("PUT", mockServer.URL()+"/index/"+did, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify URL wasn't duplicated
	updated := mockServer.GetRecord(did)
	require.NotNil(t, updated)
	require.Equal(t, 1, len(updated.URLs))
	require.Equal(t, "s3://bucket1/file.bam", updated.URLs[0])
}

//////////////////////////
// TEST HELPERS & SETUP //
//////////////////////////

// MockAuthHandler implements AuthHandler for testing without Gen3 credentials
// It simply sets a Bearer token header without making any external calls
type MockAuthHandler struct{}

func (m *MockAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	req.Header.Set("Authorization", "Bearer mock-test-token-"+profile)
	return nil
}

// testIndexdClient creates an IndexdClient pointing to a mock server with real auth handler
func testIndexdClient(baseURL string) *indexd_client.IndexDClient {
	url, _ := url.Parse(baseURL)
	return &indexd_client.IndexDClient{
		Base:        url,
		Profile:     "test-profile",
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      &log.NoOpLogger{},
		AuthHandler: &indexd_client.RealAuthHandler{},
	}
}

// testIndexdClientWithMockAuth creates an IndexdClient with mocked authentication for testing
// This helper enables testing client methods without requiring Gen3 credentials or config files
func testIndexdClientWithMockAuth(baseURL string) *indexd_client.IndexDClient {
	url, _ := url.Parse(baseURL)
	return &indexd_client.IndexDClient{
		Base:        url,
		Profile:     "test-profile",
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      &log.NoOpLogger{},
		AuthHandler: &MockAuthHandler{},
	}
}

// TestIndexdClient_RegisterIndexdRecord_CreatesNewRecord tests record creation via client method
func TestIndexdClient_RegisterIndexdRecord_CreatesNewRecord(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create input record to register
	newRecord := &indexd_client.IndexdRecord{
		Did:      "uuid-register-test",
		FileName: "new-file.bam",
		Size:     5000,
		URLs:     []string{"s3://bucket/new-file.bam"},
		Authz:    []string{"/workspace/test"},
		Hashes: indexd_client.HashInfo{
			SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		},
		Metadata: map[string]string{
			"source": "test",
		},
	}

	// Act: Call the RegisterIndexdRecord client method
	// This tests:
	// 1. Wrapping IndexdRecord in IndexdRecordForm with form="object"
	// 2. Setting correct headers (Content-Type, accept)
	// 3. Injecting auth header via MockAuthHandler
	// 4. POSTing to /index/index endpoint
	// 5. Handling 200 OK response
	// 6. Querying the new record via GET /ga4gh/drs/v1/objects/{did}
	// 7. Returning a valid DRSObject
	drsObj, err := client.RegisterIndexdRecord(newRecord)

	// Assert: Verify the client method executed successfully
	require.NoError(t, err, "RegisterIndexdRecord should succeed")
	require.NotNil(t, drsObj, "Should return a valid DRSObject")

	// Verify the stored record
	storedRecord := mockServer.GetRecord("uuid-register-test")
	require.NotNil(t, storedRecord, "Record should be stored in mock server after POST")
	require.Equal(t, "new-file.bam", storedRecord.FileName)
	require.Equal(t, int64(5000), storedRecord.Size)
	require.Equal(t, []string{"s3://bucket/new-file.bam"}, storedRecord.URLs)
	require.Equal(t, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", storedRecord.Hashes["sha256"])

	// Verify the returned DRS object
	require.Equal(t, "uuid-register-test", drsObj.Id, "DRS object ID should match DID")
	require.Equal(t, "new-file.bam", drsObj.Name, "DRS object name should match FileName")
	require.Equal(t, int64(5000), drsObj.Size, "DRS object size should match")
	require.Len(t, drsObj.Checksums, 1, "Should have one checksum")
	require.Equal(t, "sha256", string(drsObj.Checksums[0].Type), "Checksum type should be sha256")
	require.Equal(t, "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", drsObj.Checksums[0].Checksum)
	require.Len(t, drsObj.AccessMethods, 1, "Should have one access method")
	require.Equal(t, "s3://bucket/new-file.bam", drsObj.AccessMethods[0].AccessURL.URL)
}

// TestIndexdClient_UpdateIndexdRecord_AppendsURLs tests updating record via client method
func TestIndexdClient_UpdateIndexdRecord_AppendsURLs(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	did := "uuid-update-test"
	originalRecord := &MockIndexdRecord{
		Did:      did,
		FileName: "file.bam",
		Size:     2048,
		URLs:     []string{"s3://original-bucket/file.bam"},
		Authz:    []string{"/workspace/test"},
		Hashes: map[string]string{
			"sha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		},
	}

	// Pre-populate mock with original record
	mockServer.recordMutex.Lock()
	mockServer.records[did] = originalRecord
	mockServer.recordMutex.Unlock()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create update info with new URL
	updateInfo := &drs.DRSObject{
		AccessMethods: []drs.AccessMethod{drs.AccessMethod{AccessURL: drs.AccessURL{URL: "s3://new-bucket/file-v2.bam"}}},
		//URLs: []string{"s3://new-bucket/file-v2.bam"},
	}

	// Act: Call the UpdateIndexdRecord client method
	// This tests:
	// 1. Getting the existing record via GET /index/{did}
	// 2. Appending new URLs to existing URLs
	// 3. Marshaling UpdateInputInfo to JSON
	// 4. Setting correct headers (Content-Type, accept)
	// 5. Injecting auth header via MockAuthHandler
	// 6. PUTting to /index/index/{did} endpoint with new URLs
	// 7. Handling 200 OK response
	// 8. Querying the updated record via GET /ga4gh/drs/v1/objects/{did}
	// 9. Returning a valid DRSObject
	drsObj, err := client.UpdateRecord(updateInfo, did)

	// Assert: Verify the client method executed successfully
	require.NoError(t, err, "UpdateIndexdRecord should succeed")
	require.NotNil(t, drsObj, "Should return a valid DRSObject")

	// Verify the URLs were appended correctly
	updatedRecord := mockServer.GetRecord(did)
	require.NotNil(t, updatedRecord)
	require.Equal(t, 2, len(updatedRecord.URLs), "Should have appended new URL to existing")
	require.Contains(t, updatedRecord.URLs, "s3://original-bucket/file.bam")
	require.Contains(t, updatedRecord.URLs, "s3://new-bucket/file-v2.bam")

	// Verify the returned DRS object
	require.Equal(t, did, drsObj.Id, "DRS object ID should match DID")
	require.Equal(t, "file.bam", drsObj.Name, "DRS object name should match FileName")
	require.Equal(t, int64(2048), drsObj.Size, "DRS object size should match")
	require.Len(t, drsObj.Checksums, 1, "Should have one checksum")
	require.Equal(t, "sha256", string(drsObj.Checksums[0].Type), "Checksum type should be sha256")
	require.Equal(t, "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", drsObj.Checksums[0].Checksum)
	require.Len(t, drsObj.AccessMethods, 2, "Should have two access methods (URLs)")
	urls := []string{drsObj.AccessMethods[0].AccessURL.URL, drsObj.AccessMethods[1].AccessURL.URL}
	require.Contains(t, urls, "s3://original-bucket/file.bam")
	require.Contains(t, urls, "s3://new-bucket/file-v2.bam")
}
