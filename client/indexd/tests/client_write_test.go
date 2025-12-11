package indexd_tests

import (
	"testing"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/stretchr/testify/require"
)

///////////////////
// WRITE TESTS   //
///////////////////

// Integration tests for WRITE operations on IndexdClient using mock indexd server.
// These tests verify mutating operations that create, update, or delete data:
// - RegisterRecord / RegisterIndexdRecord - Create new records
// - UpdateRecord / UpdateIndexdRecord - Modify existing records
// - DeleteRecord / DeleteIndexdRecord - Remove records

// TestIndexdClient_RegisterRecord tests the high-level RegisterRecord method
// which converts a DRSObject to IndexdRecord and registers it
func TestIndexdClient_RegisterRecord(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create a DRS object to register
	drsObject := &drs.DRSObject{
		Id:   "uuid-drs-register-test",
		Name: "test-file.bam",
		Size: 3000,
		Checksums: hash.HashInfo{
			SHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		},
		AccessMethods: []drs.AccessMethod{
			{
				AccessURL: drs.AccessURL{
					URL: "s3://drs-test-bucket/test-file.bam",
				},
				Authorizations: &drs.Authorizations{
					Value: "/programs/drs-test/projects/test",
				},
			},
		},
	}

	// Act: Call RegisterRecord which should:
	// 1. Convert DRSObject to IndexdRecord
	// 2. Call RegisterIndexdRecord
	// 3. Return the registered DRSObject
	result, err := client.RegisterRecord(drsObject)

	// Assert
	require.NoError(t, err, "RegisterRecord should succeed")
	require.NotNil(t, result, "Should return a valid DRSObject")

	// Verify the record was created in the mock server
	storedRecord := mockServer.GetRecord(drsObject.Id)
	require.NotNil(t, storedRecord, "Record should be stored in mock server")
	require.Equal(t, drsObject.Name, storedRecord.FileName)
	require.Equal(t, drsObject.Size, storedRecord.Size)
	require.Contains(t, storedRecord.URLs, "s3://drs-test-bucket/test-file.bam")

	// Verify the returned DRS object matches
	require.Equal(t, drsObject.Id, result.Id)
	require.Equal(t, drsObject.Name, result.Name)
	require.Equal(t, drsObject.Size, result.Size)
}

// TestIndexdClient_RegisterRecord_MissingDID tests error handling when DID is missing
func TestIndexdClient_RegisterRecord_MissingDID(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create a DRS object without ID (mock server will reject it)
	invalidDrsObject := &drs.DRSObject{
		Name: "test-file.bam",
		Size: 3000,
		// Missing Id field - mock server should reject
	}

	// Act
	result, err := client.RegisterRecord(invalidDrsObject)

	// Assert: Should fail when registering with server (missing DID)
	require.Error(t, err, "Should fail when DID is missing")
	require.Nil(t, result)
	require.Contains(t, err.Error(), "Missing required field: did")
}

// TestIndexdClient_RegisterIndexdRecord_CreatesNewRecord tests record creation via client method
func TestIndexdClient_RegisterIndexdRecord_CreatesNewRecord(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Create input record to register
	// IndexdRecord used here is the client-side object
	// We don't use the newTestRecord helper bc that's the [mock] server-side object
	newRecord := &indexd_client.IndexdRecord{
		Did:      "uuid-register-test",
		FileName: "new-file.bam",
		Size:     5000,
		URLs:     []string{"s3://bucket/new-file.bam"},
		Authz:    []string{"/workspace/test"},
		Hashes: hash.HashInfo{
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
	require.Equal(t, newRecord.Hashes.SHA256, drsObj.Checksums.SHA256)
	require.Len(t, drsObj.AccessMethods, 1, "Should have one access method")
	require.Equal(t, newRecord.URLs[0], drsObj.AccessMethods[0].AccessURL.URL)
}

///////////////////////////////
// UpdateRecord / UpdateIndexdRecord Tests
///////////////////////////////

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
	require.Equal(t, originalRecord.Hashes["sha256"], drsObj.Checksums.SHA256)
	require.Len(t, drsObj.AccessMethods, 2, "Should have two access methods (URLs)")
	urls := []string{drsObj.AccessMethods[0].AccessURL.URL, drsObj.AccessMethods[1].AccessURL.URL}
	require.Contains(t, urls, originalRecord.URLs[0])
	require.Contains(t, urls, newURL)
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

///////////////////////////////
// DeleteRecord / DeleteIndexdRecord Tests
///////////////////////////////

// TestIndexdClient_DeleteRecord tests deleting a record by OID
func TestIndexdClient_DeleteRecord(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Pre-populate with a test record
	testHash := "1111111111111111111111111111111111111111111111111111111111111111"
	testRecord := newTestRecord("uuid-delete-by-oid",
		withTestRecordFileName("delete-me.bam"),
		withTestRecordSize(4096),
		withTestRecordHash("sha256", testHash))
	addRecordWithHashIndex(mockServer, testRecord, "sha256", testHash)

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Verify record exists before deletion
	recordBefore := mockServer.GetRecord(testRecord.Did)
	require.NotNil(t, recordBefore, "Record should exist before deletion")

	// Act: Delete by OID (which is the hash)
	err := client.DeleteRecord(testHash)

	// Assert
	require.NoError(t, err, "DeleteRecord should succeed")

	// Verify record was deleted
	recordAfter := mockServer.GetRecord(testRecord.Did)
	require.Nil(t, recordAfter, "Record should be deleted")
}

// TestIndexdClient_DeleteRecord_NotFound tests deleting a non-existent record
func TestIndexdClient_DeleteRecord_NotFound(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act: Try to delete a record that doesn't exist
	nonExistentHash := "9999999999999999999999999999999999999999999999999999999999999999"
	err := client.DeleteRecord(nonExistentHash)

	// Assert: Should return error
	require.Error(t, err, "Should fail when record doesn't exist")
	require.Contains(t, err.Error(), "No records found for OID")
}

// TestIndexdClient_DeleteRecord_NoMatchingProject tests deletion when record exists but for different project
func TestIndexdClient_DeleteRecord_NoMatchingProject(t *testing.T) {
	// Arrange
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Create a record with a DIFFERENT project authorization
	testHash := "2222222222222222222222222222222222222222222222222222222222222222"
	differentProjectAuthz := "/programs/other-program/projects/other-project"
	testRecord := newTestRecord("uuid-different-project",
		withTestRecordFileName("other-project.bam"),
		withTestRecordHash("sha256", testHash))
	testRecord.Authz = []string{differentProjectAuthz} // Override with different project
	addRecordWithHashIndex(mockServer, testRecord, "sha256", testHash)

	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Act: Try to delete - should fail because project doesn't match
	err := client.DeleteRecord(testHash)

	// Assert
	require.Error(t, err, "Should fail when no matching project")
	require.Contains(t, err.Error(), "No matching record found for project")

	// Verify record still exists (wasn't deleted)
	recordAfter := mockServer.GetRecord(testRecord.Did)
	require.NotNil(t, recordAfter, "Record should still exist")
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
