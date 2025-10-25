package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// Unit Tests for validateInputs
// (Already covered in add-url_test.go, but adding edge cases)

// tokenAuthHandler is a simple test helper that sets a Bearer token
type tokenAuthHandler struct {
	token string
}

func (t *tokenAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return nil
}

func TestValidateInputs_ConcurrentCalls(t *testing.T) {
	validS3URL := "s3://bucket/file.bam"
	validSHA256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Run concurrent validations to ensure thread safety
	errChan := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			errChan <- validateInputs(validS3URL, validSHA256)
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("concurrent validateInputs() call %d failed: %v", i, err)
		}
	}
}

// Unit Tests for getBucketDetailsWithAuth

func TestGetBucketDetailsWithAuth_Success(t *testing.T) {
	// Test that getBucketDetailsWithAuth properly uses the AuthHandler
	authHeaderValue := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request
		if r.Method != "GET" {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		// Capture the auth header set by the handler
		authHeaderValue = r.Header.Get("Authorization")

		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
					Programs:    []string{"program1"},
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	ctx := context.Background()
	mockAuth := &MockAuthHandler{}
	result, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", mockAuth, server.Client())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Verify the MockAuthHandler added the expected header
	expectedAuthHeader := "Bearer mock-test-token-test-profile"
	if authHeaderValue != expectedAuthHeader {
		t.Errorf("Expected auth header '%s', got '%s'", expectedAuthHeader, authHeaderValue)
	}

	if result.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", result.Region)
	}

	if result.EndpointURL != "https://s3.amazonaws.com" {
		t.Errorf("Expected endpoint https://s3.amazonaws.com, got %s", result.EndpointURL)
	}
}

func TestGetBucketDetailsWithAuth_BucketMissing(t *testing.T) {
	// Test that missing bucket returns proper error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"other-bucket": {
					Region:      "us-east-1",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := getBucketDetailsWithAuth(ctx, "missing-bucket", server.URL, "test-profile", nil, server.Client())

	if err == nil {
		t.Fatal("Expected 'bucket not found' error, got nil")
	}

	if !strings.Contains(err.Error(), "bucket not found") {
		t.Errorf("Expected 'bucket not found' error, got: %v", err)
	}
}

func TestGetBucketDetailsWithAuth_MissingFields(t *testing.T) {
	tests := []struct {
		name       string
		bucket     S3Bucket
		wantErrMsg string
	}{
		{
			name: "missing region",
			bucket: S3Bucket{
				EndpointURL: "https://s3.amazonaws.com",
				Region:      "",
			},
			wantErrMsg: "endpoint_url or region not found",
		},
		{
			name: "missing endpoint",
			bucket: S3Bucket{
				EndpointURL: "",
				Region:      "us-west-2",
			},
			wantErrMsg: "endpoint_url or region not found",
		},
		{
			name: "missing both",
			bucket: S3Bucket{
				EndpointURL: "",
				Region:      "",
			},
			wantErrMsg: "endpoint_url or region not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				response := S3BucketsResponse{
					S3Buckets: map[string]S3Bucket{
						"test-bucket": tt.bucket,
					},
				}
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			ctx := context.Background()
			_, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", nil, server.Client())

			if err == nil {
				t.Fatal("Expected error for missing fields, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("Expected error containing '%s', got: %v", tt.wantErrMsg, err)
			}
		})
	}
}

func TestGetBucketDetailsWithAuth_Non200Status(t *testing.T) {
	// Test that any non-200 status code returns an error
	// All non-200 codes follow the same error path, so one representative case is sufficient
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", nil, server.Client())

	if err == nil {
		t.Fatal("Expected error for non-200 status, got nil")
	}

	if !strings.Contains(err.Error(), "unexpected status code: 404") {
		t.Errorf("Expected error containing 'unexpected status code', got: %v", err)
	}
}

func TestGetBucketDetailsWithAuth_WithToken(t *testing.T) {
	// Test that a token-based auth handler properly sets the token
	expectedToken := "test-auth-token-12345"
	tokenReceived := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the Authorization header
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			tokenReceived = strings.TrimPrefix(authHeader, "Bearer ")
		}

		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	ctx := context.Background()
	tokenAuth := &tokenAuthHandler{token: expectedToken}
	_, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", tokenAuth, server.Client())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if tokenReceived != expectedToken {
		t.Errorf("Expected token '%s', got '%s'", expectedToken, tokenReceived)
	}
}

func TestGetBucketDetailsWithAuth_NoAuthHandler(t *testing.T) {
	// Test that nil AuthHandler works (no auth added)
	authHeaderPresent := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if Authorization header is present
		if r.Header.Get("Authorization") != "" {
			authHeaderPresent = true
		}

		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", nil, server.Client())

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if authHeaderPresent {
		t.Error("Authorization header should not be present when AuthHandler is nil")
	}
}

func TestGetBucketDetailsWithAuth_AuthHandlerError(t *testing.T) {
	// Test that errors from AuthHandler are properly propagated
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not reach here if auth fails
		t.Error("Server handler should not be called when auth fails")
	}))
	defer server.Close()

	// Create a mock auth handler that returns an error
	errorAuth := &errorMockAuthHandler{err: errors.New("auth failed")}

	ctx := context.Background()
	_, err := getBucketDetailsWithAuth(ctx, "test-bucket", server.URL, "test-profile", errorAuth, server.Client())

	if err == nil {
		t.Fatal("Expected error from auth handler, got nil")
	}

	if !strings.Contains(err.Error(), "failed to add authentication") {
		t.Errorf("Expected error to mention authentication failure, got: %v", err)
	}

	if !strings.Contains(err.Error(), "auth failed") {
		t.Errorf("Expected error to contain original auth error, got: %v", err)
	}
}

// errorMockAuthHandler is a test helper that always returns an error
type errorMockAuthHandler struct {
	err error
}

func (e *errorMockAuthHandler) AddAuthHeader(req *http.Request, profile string) error {
	return e.err
}

// Unit Tests for S3BucketsResponse structure
func TestS3BucketsResponse_UnmarshalValid(t *testing.T) {
	jsonData := `{
		"S3_BUCKETS": {
			"bucket1": {
				"region": "us-west-2",
				"endpoint_url": "https://s3.amazonaws.com",
				"programs": ["program1", "program2"]
			}
		},
		"GS_BUCKETS": {}
	}`

	var response S3BucketsResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal S3BucketsResponse: %v", err)
	}

	if len(response.S3Buckets) != 1 {
		t.Errorf("Expected 1 S3 bucket, got %d", len(response.S3Buckets))
	}

	bucket, exists := response.S3Buckets["bucket1"]
	if !exists {
		t.Fatalf("Expected bucket1 to exist")
	}

	if bucket.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", bucket.Region)
	}

	if bucket.EndpointURL != "https://s3.amazonaws.com" {
		t.Errorf("Expected endpoint https://s3.amazonaws.com, got %s", bucket.EndpointURL)
	}

	if len(bucket.Programs) != 2 {
		t.Errorf("Expected 2 programs, got %d", len(bucket.Programs))
	}
}

func TestS3BucketsResponse_EmptyBuckets(t *testing.T) {
	jsonData := `{
		"S3_BUCKETS": {},
		"GS_BUCKETS": {}
	}`

	var response S3BucketsResponse
	err := json.Unmarshal([]byte(jsonData), &response)
	if err != nil {
		t.Fatalf("Failed to unmarshal empty S3BucketsResponse: %v", err)
	}

	if len(response.S3Buckets) != 0 {
		t.Errorf("Expected 0 S3 buckets, got %d", len(response.S3Buckets))
	}
}

// Unit Tests for S3Bucket structure

func TestS3Bucket_MissingOptionalFields(t *testing.T) {
	jsonData := `{
		"region": "us-west-2"
	}`

	var bucket S3Bucket
	err := json.Unmarshal([]byte(jsonData), &bucket)
	if err != nil {
		t.Fatalf("Failed to unmarshal S3Bucket: %v", err)
	}

	if bucket.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", bucket.Region)
	}

	if bucket.EndpointURL != "" {
		t.Errorf("Expected empty endpoint, got %s", bucket.EndpointURL)
	}

	if len(bucket.Programs) != 0 {
		t.Errorf("Expected 0 programs, got %d", len(bucket.Programs))
	}
}

// Unit Tests for fetchS3Metadata (now tested through AddURL public API)

// NOTE: Direct calls to private fetchS3Metadata() are removed.
// Input validation is now tested through the public AddURL() API above.
// This follows Go best practices: test the public API, not private helpers.

// For testing S3 metadata fetching with mock clients, use:
//   AddURL(s3URL, sha256, key, secret, region, endpoint, WithS3Client(mockS3), WithHTTPClient(mockHTTP))

// Unit Tests for upsertIndexdRecord with mocking

func TestUpsertIndexdRecordWithClient_UpdateExistingRecord(t *testing.T) {
	// Test case 1: the record exists for the project, it updates the URL

	// Setup mock indexd server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Create client with mock auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Setup test data
	projectId := "testprogram-testproject"
	sha256 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	url1 := "s3://bucket1/file1.bam"
	url2 := "s3://bucket2/file2.bam" // Different URL
	fileSize := int64(1000)
	modifiedDate := "2024-01-01"

	// Pre-populate the mock server with an existing record for this project
	existingUUID := DrsUUID(projectId, sha256)
	authzStr := "/programs/testprogram/projects/testproject"

	existingRecord := &IndexdRecord{
		Did:      existingUUID,
		FileName: "file1.bam",
		Hashes:   HashInfo{SHA256: sha256},
		Size:     fileSize,
		URLs:     []string{url1},
		Authz:    []string{authzStr},
		Metadata: map[string]string{"remote": "true"},
	}

	_, err := client.RegisterIndexdRecord(existingRecord)
	if err != nil {
		t.Fatalf("Failed to pre-populate mock server: %v", err)
	}

	// Verify the record was created
	records, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query existing records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	// Now upsert with a different URL - should update the existing record
	err = upsertIndexdRecordWithClient(client, projectId, url2, sha256, fileSize, modifiedDate)
	if err != nil {
		t.Fatalf("upsertIndexdRecordWithClient failed: %v", err)
	}

	// Verify the record was updated with the new URL
	updatedRecords, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query updated records: %v", err)
	}

	if len(updatedRecords) != 1 {
		t.Fatalf("Expected 1 record after update, got %d", len(updatedRecords))
	}

	updatedRecord := updatedRecords[0]

	// Should have both URLs now
	if len(updatedRecord.URLs) != 2 {
		t.Errorf("Expected 2 URLs after update, got %d: %v", len(updatedRecord.URLs), updatedRecord.URLs)
	}

	if !slices.Contains(updatedRecord.URLs, url1) {
		t.Errorf("Expected original URL %s to still be present", url1)
	}

	if !slices.Contains(updatedRecord.URLs, url2) {
		t.Errorf("Expected new URL %s to be added", url2)
	}

	// Verify the DID hasn't changed
	if updatedRecord.Did != existingUUID {
		t.Errorf("Expected DID to remain %s, got %s", existingUUID, updatedRecord.Did)
	}
}

func TestUpsertIndexdRecordWithClient_CreateNewRecordDifferentProject(t *testing.T) {
	// Test case 2: a record exists but it is not for the same project, so a new record is created

	// Setup mock indexd server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Create client with mock auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Setup test data
	project1 := "program1-project1"
	project2 := "program2-project2" // Different project
	sha256 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	url1 := "s3://bucket1/shared-file.bam"
	url2 := "s3://bucket2/shared-file.bam"
	fileSize := int64(2000)
	modifiedDate := "2024-01-02"

	// Pre-populate with a record for project1
	uuid1 := DrsUUID(project1, sha256)
	authz1 := "/programs/program1/projects/project1"

	existingRecord := &IndexdRecord{
		Did:      uuid1,
		FileName: "shared-file.bam",
		Hashes:   HashInfo{SHA256: sha256},
		Size:     fileSize,
		URLs:     []string{url1},
		Authz:    []string{authz1},
		Metadata: map[string]string{"remote": "true"},
	}

	_, err := client.RegisterIndexdRecord(existingRecord)
	if err != nil {
		t.Fatalf("Failed to pre-populate mock server: %v", err)
	}

	// Verify the first record exists
	records, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query existing records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("Expected 1 record initially, got %d", len(records))
	}

	// Now upsert with project2 - should create a NEW record (not update the existing one)
	err = upsertIndexdRecordWithClient(client, project2, url2, sha256, fileSize, modifiedDate)
	if err != nil {
		t.Fatalf("upsertIndexdRecordWithClient failed: %v", err)
	}

	// Verify we now have 2 records for the same hash
	allRecords, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query all records: %v", err)
	}

	if len(allRecords) != 2 {
		t.Fatalf("Expected 2 records for same hash (different projects), got %d", len(allRecords))
	}

	// Verify the DIDs are different
	uuid2 := DrsUUID(project2, sha256)
	authz2 := "/programs/program2/projects/project2"

	foundProject1Record := false
	foundProject2Record := false

	for _, record := range allRecords {
		if record.Did == uuid1 && slices.Contains(record.Authz, authz1) {
			foundProject1Record = true
			// Original record should be unchanged
			if len(record.URLs) != 1 || record.URLs[0] != url1 {
				t.Errorf("Project1 record was modified: %v", record.URLs)
			}
		}
		if record.Did == uuid2 && slices.Contains(record.Authz, authz2) {
			foundProject2Record = true
			// New record should have the new URL
			if len(record.URLs) != 1 || record.URLs[0] != url2 {
				t.Errorf("Project2 record has wrong URL: %v", record.URLs)
			}
		}
	}

	if !foundProject1Record {
		t.Error("Project1 record not found")
	}
	if !foundProject2Record {
		t.Error("Project2 record not found")
	}

	// Verify the DIDs are actually different (different projects = different UUIDs)
	if uuid1 == uuid2 {
		t.Error("Expected different DIDs for different projects, but they're the same")
	}
}

func TestUpsertIndexdRecordWithClient_IdempotentSameURL(t *testing.T) {
	// Test that upserting the same URL twice is idempotent (no duplicate URLs)

	// Setup mock indexd server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Create client with mock auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Setup test data
	projectId := "testprogram-testproject"
	sha256 := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	url := "s3://bucket1/file.bam"
	fileSize := int64(3000)
	modifiedDate := "2024-01-03"

	// First upsert - creates the record
	err := upsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate)
	if err != nil {
		t.Fatalf("First upsertIndexdRecordWithClient failed: %v", err)
	}

	// Second upsert - same URL, should be idempotent
	err = upsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate)
	if err != nil {
		t.Fatalf("Second upsertIndexdRecordWithClient failed: %v", err)
	}

	// Verify there's still only one record with one URL
	records, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query records: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record, got %d", len(records))
	}

	record := records[0]

	// Should still have exactly 1 URL (not duplicated)
	if len(record.URLs) != 1 {
		t.Errorf("Expected 1 URL (idempotent), got %d: %v", len(record.URLs), record.URLs)
	}

	if record.URLs[0] != url {
		t.Errorf("Expected URL %s, got %s", url, record.URLs[0])
	}
}

func TestUpsertIndexdRecordWithClient_CreateNewRecordNoExisting(t *testing.T) {
	// Test creating a brand new record when no records exist for the hash

	// Setup mock indexd server
	mockServer := NewMockIndexdServer(t)
	defer mockServer.Close()

	// Create client with mock auth
	client := testIndexdClientWithMockAuth(mockServer.URL())

	// Setup test data
	projectId := "newprogram-newproject" // Fixed format: <program>-<project>
	sha256 := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	url := "s3://new-bucket/new-file.bam"
	fileSize := int64(4000)
	modifiedDate := "2024-01-04"

	// Verify no records exist initially
	records, err := client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query initial records: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("Expected 0 records initially, got %d", len(records))
	}

	// Create the record
	err = upsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate)
	if err != nil {
		t.Fatalf("upsertIndexdRecordWithClient failed: %v", err)
	}

	// Verify the record was created
	records, err = client.GetObjectsByHash("sha256", sha256)
	if err != nil {
		t.Fatalf("Failed to query created records: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("Expected 1 record after creation, got %d", len(records))
	}

	record := records[0]
	expectedUUID := DrsUUID(projectId, sha256)
	expectedAuthz := "/programs/newprogram/projects/newproject"

	// Verify record properties
	if record.Did != expectedUUID {
		t.Errorf("Expected DID %s, got %s", expectedUUID, record.Did)
	}

	if len(record.URLs) != 1 || record.URLs[0] != url {
		t.Errorf("Expected URLs [%s], got %v", url, record.URLs)
	}

	if !slices.Contains(record.Authz, expectedAuthz) {
		t.Errorf("Expected authz to contain %s, got %v", expectedAuthz, record.Authz)
	}

	if record.Size != fileSize {
		t.Errorf("Expected size %d, got %d", fileSize, record.Size)
	}

	// Verify hash
	if record.Hashes.SHA256 != sha256 {
		t.Errorf("Expected hash %s, got %s", sha256, record.Hashes.SHA256)
	}
}

// Legacy test kept for reference
func TestUpsertIndexdRecord_Integration(t *testing.T) {
	// This is better as an integration test due to dependencies
	t.Skip("Moved to integration tests - requires config and indexd client")
}

// Unit Tests for FindMatchingRecord

func TestFindMatchingRecord_EmptyRecords(t *testing.T) {
	records := []OutputInfo{}
	projectId := "test-project"

	result, err := FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("FindMatchingRecord() expected nil for empty records, got %v", result)
	}
}

func TestFindMatchingRecord_SingleMatch(t *testing.T) {
	records := []OutputInfo{
		{
			Did:   "uuid-1",
			Authz: []string{"/programs/test/projects/project"},
			URLs:  []string{"s3://bucket/file1.txt"},
		},
	}
	projectId := "test-project"

	result, err := FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("FindMatchingRecord() expected a match, got nil")
	}
	if result.Did != "uuid-1" {
		t.Errorf("FindMatchingRecord() expected Did uuid-1, got %s", result.Did)
	}
}

func TestFindMatchingRecord_MultipleRecordsFirstMatch(t *testing.T) {
	records := []OutputInfo{
		{
			Did:   "uuid-1",
			Authz: []string{"/programs/test/projects/project"},
			URLs:  []string{"s3://bucket/file1.txt"},
		},
		{
			Did:   "uuid-2",
			Authz: []string{"/programs/other/projects/other"},
			URLs:  []string{"s3://bucket/file2.txt"},
		},
		{
			Did:   "uuid-3",
			Authz: []string{"/programs/test/projects/project"},
			URLs:  []string{"s3://bucket/file3.txt"},
		},
	}
	projectId := "test-project"

	result, err := FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("FindMatchingRecord() expected a match, got nil")
	}
	// Should return the first matching record
	if result.Did != "uuid-1" {
		t.Errorf("FindMatchingRecord() expected Did uuid-1, got %s", result.Did)
	}
}

func TestFindMatchingRecord_NoMatch(t *testing.T) {
	records := []OutputInfo{
		{
			Did:   "uuid-1",
			Authz: []string{"/programs/other/projects/other"},
			URLs:  []string{"s3://bucket/file1.txt"},
		},
	}
	projectId := "test-project"

	result, err := FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("FindMatchingRecord() expected nil for no match, got %v", result)
	}
}

// Unit Tests for DrsUUID

func TestDrsUUID_ReproducibleGeneration(t *testing.T) {
	projectID := "test-project"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Generate UUID multiple times
	uuids := make([]string, 100)
	for i := 0; i < 100; i++ {
		uuids[i] = DrsUUID(projectID, sha256)
	}

	// All UUIDs should be identical
	firstUUID := uuids[0]
	for i, uuid := range uuids {
		if uuid != firstUUID {
			t.Errorf("DrsUUID() not consistent at index %d: expected %s, got %s", i, firstUUID, uuid)
		}
	}
}

func TestDrsUUID_DifferentProjects(t *testing.T) {
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	uuid1 := DrsUUID("project-1", sha256)
	uuid2 := DrsUUID("project-2", sha256)

	if uuid1 == uuid2 {
		t.Errorf("DrsUUID() should generate different UUIDs for different projects")
	}
}

func TestDrsUUID_DifferentHashes(t *testing.T) {
	projectID := "test-project"

	uuid1 := DrsUUID(projectID, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	uuid2 := DrsUUID(projectID, "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	if uuid1 == uuid2 {
		t.Errorf("DrsUUID() should generate different UUIDs for different hashes")
	}
}

// Unit Tests for customEndpointResolver
func TestCustomEndpointResolver(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		service  string
		region   string
	}{
		{
			name:     "S3 endpoint",
			endpoint: "https://s3.amazonaws.com",
			service:  "s3",
			region:   "us-west-2",
		},
		{
			name:     "Custom endpoint",
			endpoint: "https://custom.s3.endpoint.com",
			service:  "s3",
			region:   "us-east-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &customEndpointResolver{endpoint: tt.endpoint}
			endpoint, err := resolver.ResolveEndpoint(tt.service, tt.region)

			if err != nil {
				t.Errorf("ResolveEndpoint() unexpected error: %v", err)
			}

			if endpoint.URL != tt.endpoint {
				t.Errorf("Expected endpoint %s, got %s", tt.endpoint, endpoint.URL)
			}
		})
	}
}

// Unit Tests for error scenarios
func TestAddURL_InvalidInputsEarlyReturn(t *testing.T) {
	tests := []struct {
		name   string
		s3URL  string
		sha256 string
	}{
		{
			name:   "invalid S3 URL",
			s3URL:  "http://bucket/file.txt",
			sha256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:   "invalid SHA256",
			s3URL:  "s3://bucket/file.txt",
			sha256: "invalid-sha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := AddURL(tt.s3URL, tt.sha256, "key", "secret", "us-west-2", "https://s3.amazonaws.com")
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tt.name)
			}
		})
	}
}

// Table-driven test for S3 URL parsing edge cases
func TestS3URLParsing_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		s3URL       string
		expectError bool
		description string
	}{
		{
			name:        "URL with spaces",
			s3URL:       "s3://bucket/path with spaces/file.txt",
			expectError: false,
			description: "S3 URLs can contain spaces in the path",
		},
		{
			name:        "URL with special characters",
			s3URL:       "s3://bucket/path/!@#$%^&*().txt",
			expectError: false,
			description: "S3 URLs can contain special characters",
		},
		{
			name:        "URL with Unicode",
			s3URL:       "s3://bucket/path/文件.txt",
			expectError: false,
			description: "S3 URLs can contain Unicode characters",
		},
		{
			name:        "Very long path",
			s3URL:       "s3://bucket/" + strings.Repeat("a/", 100) + "file.txt",
			expectError: false,
			description: "S3 URLs can have very long paths",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInputs(tt.s3URL, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
			if (err != nil) != tt.expectError {
				t.Errorf("validateInputs() for %s error = %v, expectError %v", tt.description, err, tt.expectError)
			}
		})
	}
}
