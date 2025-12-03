package indexd_tests

import (
	"net/http"
	"net/url"
	"testing"

	"log"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/stretchr/testify/require"
)

///////////////////
// INTEGRATION  //
// TESTS        //
///////////////////

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
	results, err := client.GetObjectsByHash("sha256", sha256)

	// Assert: Verify client method works end-to-end
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify correct record was returned
	record := results[0]
	require.Equal(t, testRecord.Did, record.Id)
	require.Equal(t, testRecord.Size, record.Size)
	require.Equal(t, sha256, record.Checksums[0].Checksum)
	require.Equal(t, drs.ChecksumTypeSHA256, record.Checksums[0].Type)

	require.Equal(t, testRecord.URLs[0], record.AccessMethods[0].AccessURL.URL)
	require.Equal(t, testRecord.Authz[0], record.AccessMethods[0].Authorizations.Value)

	// Test: Query with non-existent hash
	emptyResults, err := client.GetObjectsByHash("sha256", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	require.NoError(t, err)
	require.Len(t, emptyResults, 0)
}

// TestIndexdClient_DeleteIndexdRecord_Removes tests record deletion via client method
func TestIndexdClient_DeleteIndexdRecord_Removes(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	testRecord := newTestRecord("uuid-delete-test", withTestRecordURLs("s3://bucket/file.bam"))
	addRecordToMockServer(mockServer, testRecord)

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Delete record via client method
	err := client.DeleteIndexdRecord(testRecord.Did)

	require.NoError(t, err)

	// Verify it's gone
	deletedRecord := mockServer.GetRecord(testRecord.Did)
	require.Nil(t, deletedRecord)
}

// TestIndexdClient_UpdateIndexdRecord_Idempotent tests URL appending idempotency via client method
func TestIndexdClient_UpdateIndexdRecord_Idempotent(t *testing.T) {
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	originalRecord := newTestRecord("uuid-update-idempotent",
		withTestRecordURLs("s3://bucket1/file.bam"),
		withTestRecordHash("sha256", "aaaa..."))
	addRecordToMockServer(mockServer, originalRecord)

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create update info with same URL (should be idempotent)
	updateInfo := &drs.DRSObject{
		AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: originalRecord.URLs[0]}}},
	}

	// call the UpdateRecord client method
	drsObj, err := client.UpdateRecord(updateInfo, originalRecord.Did)
	require.NoError(t, err)

	// Verify URL wasn't duplicated
	updated := mockServer.GetRecord(drsObj.Id)
	require.NotNil(t, updated)
	require.Equal(t, 1, len(updated.URLs))
	require.Equal(t, originalRecord.URLs[0], updated.URLs[0])
}

//////////////////////////
// TEST HELPERS & SETUP //
//////////////////////////

// Test data constants
const (
	testSHA256Hash    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDefaultBucket = "test-bucket"
	testDefaultAuthz  = "/programs/test/projects/project"
)

// newTestRecord creates a standard test record with sensible defaults
// Use withTestRecord* options to customize specific fields
func newTestRecord(did string, opts ...func(*MockIndexdRecord)) *MockIndexdRecord {
	record := &MockIndexdRecord{
		Did:      did,
		FileName: "test.bam",
		Size:     1024,
		Hashes: map[string]string{
			"sha256": testSHA256Hash,
		},
		URLs:  []string{"s3://" + testDefaultBucket + "/test.bam"},
		Authz: []string{testDefaultAuthz},
	}

	// Apply options
	for _, opt := range opts {
		opt(record)
	}

	return record
}

// withTestRecordSize sets a custom size for a test record
func withTestRecordSize(size int64) func(*MockIndexdRecord) {
	return func(r *MockIndexdRecord) {
		r.Size = size
	}
}

// withTestRecordFileName sets a custom file name for a test record
func withTestRecordFileName(fileName string) func(*MockIndexdRecord) {
	return func(r *MockIndexdRecord) {
		r.FileName = fileName
	}
}

// withTestRecordURLs sets custom URLs for a test record
func withTestRecordURLs(urls ...string) func(*MockIndexdRecord) {
	return func(r *MockIndexdRecord) {
		r.URLs = urls
	}
}

// withTestRecordHash sets a custom hash for a test record
func withTestRecordHash(hashType, hash string) func(*MockIndexdRecord) {
	return func(r *MockIndexdRecord) {
		if r.Hashes == nil {
			r.Hashes = make(map[string]string)
		}
		r.Hashes[hashType] = hash
	}
}

// addRecordToMockServer is a helper to add a record to the mock server with proper locking
func addRecordToMockServer(mockServer *MockIndexdServer, record *MockIndexdRecord) {
	mockServer.recordMutex.Lock()
	mockServer.records[record.Did] = record
	mockServer.recordMutex.Unlock()
}

// addRecordWithHashIndex adds a record to the mock server and indexes it by hash
func addRecordWithHashIndex(mockServer *MockIndexdServer, record *MockIndexdRecord, hashType, hash string) {
	mockServer.recordMutex.Lock()
	mockServer.records[record.Did] = record
	key := hashType + ":" + hash
	mockServer.hashIndex[key] = []string{record.Did}
	mockServer.recordMutex.Unlock()
}

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
		Remote:      "test-remote",
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      log.Default(),
		AuthHandler: &indexd_client.RealAuthHandler{},
	}
}

// testIndexdClientWithMockAuth creates an IndexdClient with mocked authentication for testing
// This helper enables testing client methods without requiring Gen3 credentials or config files
func testIndexdClientWithMockAuth(baseURL string) *indexd_client.IndexDClient {
	url, _ := url.Parse(baseURL)
	return &indexd_client.IndexDClient{
		Base:        url,
		Remote:      "test-profile",
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      log.Default(),
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

	// Verify the stored record matches input
	storedRecord := mockServer.GetRecord(newRecord.Did)
	require.NotNil(t, storedRecord, "Record should be stored in mock server after POST")
	require.Equal(t, newRecord.FileName, storedRecord.FileName)
	require.Equal(t, newRecord.Size, storedRecord.Size)
	require.Equal(t, newRecord.URLs, storedRecord.URLs)
	require.Equal(t, newRecord.Hashes.SHA256, storedRecord.Hashes["sha256"])

	// Verify the returned DRS object matches input
	require.Equal(t, newRecord.Did, drsObj.Id, "DRS object ID should match DID")
	require.Equal(t, newRecord.FileName, drsObj.Name, "DRS object name should match FileName")
	require.Equal(t, newRecord.Size, drsObj.Size, "DRS object size should match")
	require.Len(t, drsObj.Checksums, 1, "Should have one checksum")
	require.Equal(t, "sha256", string(drsObj.Checksums[0].Type), "Checksum type should be sha256")
	require.Equal(t, newRecord.Hashes.SHA256, drsObj.Checksums[0].Checksum)
	require.Len(t, drsObj.AccessMethods, 1, "Should have one access method")
	require.Equal(t, newRecord.URLs[0], drsObj.AccessMethods[0].AccessURL.URL)
}

// TestIndexdClient_UpdateIndexdRecord_AppendsURLs tests updating record via client method
func TestIndexdClient_UpdateIndexdRecord_AppendsURLs(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	originalRecord := newTestRecord("uuid-update-test",
		withTestRecordFileName("file.bam"),
		withTestRecordSize(2048),
		withTestRecordURLs("s3://original-bucket/file.bam"),
		withTestRecordHash("sha256", "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"))
	addRecordToMockServer(mockServer, originalRecord)

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create update info with new URL
	newURL := "s3://new-bucket/file-v2.bam"
	updateInfo := &drs.DRSObject{
		AccessMethods: []drs.AccessMethod{{AccessURL: drs.AccessURL{URL: newURL}}},
	}

	// Act: Call the UpdateRecord client method
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
	drsObj, err := client.UpdateRecord(updateInfo, originalRecord.Did)

	// Assert: Verify the client method executed successfully
	require.NoError(t, err, "UpdateIndexdRecord should succeed")
	require.NotNil(t, drsObj, "Should return a valid DRSObject")

	// Verify the URLs were appended correctly
	updatedRecord := mockServer.GetRecord(originalRecord.Did)
	require.NotNil(t, updatedRecord)
	require.Equal(t, 2, len(updatedRecord.URLs), "Should have appended new URL to existing")
	require.Contains(t, updatedRecord.URLs, originalRecord.URLs[0])
	require.Contains(t, updatedRecord.URLs, newURL)

	// Verify the returned DRS object
	require.Equal(t, originalRecord.Did, drsObj.Id, "DRS object ID should match DID")
	require.Equal(t, originalRecord.FileName, drsObj.Name, "DRS object name should match FileName")
	require.Equal(t, originalRecord.Size, drsObj.Size, "DRS object size should match")
	require.Len(t, drsObj.Checksums, 1, "Should have one checksum")
	require.Equal(t, "sha256", string(drsObj.Checksums[0].Type), "Checksum type should be sha256")
	require.Equal(t, originalRecord.Hashes["sha256"], drsObj.Checksums[0].Checksum)
	require.Len(t, drsObj.AccessMethods, 2, "Should have two access methods (URLs)")
	urls := []string{drsObj.AccessMethods[0].AccessURL.URL, drsObj.AccessMethods[1].AccessURL.URL}
	require.Contains(t, urls, originalRecord.URLs[0])
	require.Contains(t, urls, newURL)
}
