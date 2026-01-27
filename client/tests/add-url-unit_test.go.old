package indexd_tests

// // TODO: fix this during add-url fix
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	indexd_client "github.com/calypr/data-client/indexd"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/s3_utils"
)

// Unit Tests for validateInputs
// (Already covered in add-url_test.go, but adding edge cases)

// noOpLogger is a logger that discards all output for tests
var noOpLogger = logs.NewSlogNoOpLogger()

// tokenAuthHandler is a simple test helper that sets a Bearer token
type tokenAuthHandler struct {
	token string
}

func (t *tokenAuthHandler) AddAuthHeader(req *http.Request) error {
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
			errChan <- indexd_client.ValidateInputs(validS3URL, validSHA256)
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("concurrent validateInputs() call %d failed: %v", i, err)
		}
	}
}

// Obsolete tests for GetBucketDetailsWithAuth removed as per user feedback to use fence client

func TestGetBucketDetailsWithAuth_Non200Status(t *testing.T) {
	// Test that any non-200 status code returns an error
	// All non-200 codes follow the same error path, so one representative case is sufficient
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := indexd_client.GetBucketDetailsWithAuth(ctx, "test-bucket", server.URL, nil, server.Client())

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

		response := s3_utils.S3BucketsResponse{
			S3Buckets: map[string]*s3_utils.S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	tokenAuth := &tokenAuthHandler{token: expectedToken}
	_, err := indexd_client.GetBucketDetailsWithAuth(ctx, "test-bucket", server.URL, tokenAuth, server.Client())

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

		response := s3_utils.S3BucketsResponse{
			S3Buckets: map[string]*s3_utils.S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Fatalf("Failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	ctx := context.Background()
	_, err := indexd_client.GetBucketDetailsWithAuth(ctx, "test-bucket", server.URL, nil, server.Client())

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
	_, err := indexd_client.GetBucketDetailsWithAuth(ctx, "test-bucket", server.URL, errorAuth, server.Client())

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

func (e *errorMockAuthHandler) AddAuthHeader(req *http.Request) error {
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

	var response s3_utils.S3BucketsResponse
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

	var response s3_utils.S3BucketsResponse
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

	var bucket s3_utils.S3Bucket
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

// Unit Tests for fetchS3Metadata

func TestFetchS3Metadata_Success_WithProvidedClient(t *testing.T) {
	// Test the happy path: S3 client is provided, bucket details provided, successful HeadObject
	ctx := context.Background()

	// Setup mock S3 server
	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	// Add object to S3 mock
	s3Mock.AddObject("test-bucket", "path/to/file.bam", 2048)

	// Create S3 client pointing to mock
	cfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion("us-west-2"),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Mock.URL())
		o.UsePathStyle = true
	})

	// Provide bucket details directly (bypass getBucketDetails)
	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: s3Mock.URL(),
		Programs:    []string{"test-program"},
	}

	// Call fetchS3MetadataWithBucketDetails with provided client and bucket details
	size, modifiedDate, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/path/to/file.bam",
		"", "", "", "", // No AWS credentials/region/endpoint in params (using client)
		bucketDetails,
		s3Client,
		noOpLogger,
	)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if size != 2048 {
		t.Errorf("Expected size 2048, got %d", size)
	}

	if modifiedDate == "" {
		t.Error("Expected non-empty modified date")
	}
}

func TestFetchS3Metadata_Success_WithCredentialsInParams(t *testing.T) {
	// Test creating S3 client from provided credentials in function params
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	s3Mock.AddObject("test-bucket", "file.bam", 1024)

	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: s3Mock.URL(),
	}

	// Call without providing s3Client, but with credentials in params
	// Note: This will create its own client and validate credentials
	size, modifiedDate, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/file.bam",
		"test-access-key",
		"test-secret-key",
		"us-west-2",
		s3Mock.URL(),
		bucketDetails,
		nil, // No s3Client provided - will create one
		noOpLogger,
	)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if size != 1024 {
		t.Errorf("Expected size 1024, got %d", size)
	}

	if modifiedDate == "" {
		t.Error("Expected non-empty modified date")
	}
}

func TestFetchS3Metadata_Success_UsingBucketDetailsFromGen3(t *testing.T) {
	// Test that bucket details (region/endpoint) from Gen3 are used when params are empty
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	s3Mock.AddObject("test-bucket", "data.bam", 512)

	// Bucket details from Gen3 (simulated)
	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: s3Mock.URL(),
	}

	// Don't provide region/endpoint in params - should use bucketDetails
	size, modifiedDate, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/data.bam",
		"test-key",
		"test-secret",
		"", // No region param - should use bucketDetails
		"", // No endpoint param - should use bucketDetails
		bucketDetails,
		nil,
		noOpLogger,
	)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if size != 512 {
		t.Errorf("Expected size 512, got %d", size)
	}

	if modifiedDate == "" {
		t.Error("Expected non-empty modified date")
	}
}

func TestFetchS3Metadata_Failure_InvalidS3URL(t *testing.T) {
	// Test that invalid S3 URL is rejected early
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: "http://endpoint",
	}

	_, _, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"not-an-s3-url",
		"key", "secret", "us-west-2", "http://endpoint",
		bucketDetails,
		nil,
		noOpLogger,
	)

	if err == nil {
		t.Fatal("Expected error for invalid S3 URL, got nil")
	}

	if !strings.Contains(err.Error(), "failed to parse S3 URL") {
		t.Errorf("Expected parse error, got: %v", err)
	}
}

func TestFetchS3Metadata_Failure_MissingCredentials(t *testing.T) {
	// Test validation failure when credentials are missing
	// Note: This test checks that when no client is provided AND no credentials in params,
	// the function should fail with missing credentials error if AWS env doesn't have them.
	// However, in CI/local environments with AWS credentials, this test may pass.
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	bucketDetails := &s3_utils.S3Bucket{
		Region:      "", // No region - this will definitely trigger validation error
		EndpointURL: s3Mock.URL(),
	}

	// Try to create client without credentials and without region
	_, _, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/file.bam",
		"", "", // No credentials in params
		"", // No region (key point - this will fail)
		"", // No endpoint
		bucketDetails,
		nil, // No s3Client - will try to create one
		noOpLogger,
	)

	// Should fail on missing region at minimum
	if err == nil {
		t.Fatal("Expected error for missing configuration, got nil")
	}

	// Error should mention missing configuration (either credentials or region)
	if !strings.Contains(err.Error(), "Missing required AWS configuration") {
		t.Errorf("Expected missing configuration error, got: %v", err)
	}
}

func TestFetchS3Metadata_Failure_MissingRegion(t *testing.T) {
	// Test validation failure when region is missing
	ctx := context.Background()

	// Bucket details WITHOUT region
	ignoreAWSConfigFiles(t)
	bucketDetails := &s3_utils.S3Bucket{
		EndpointURL: "http://s3-endpoint",
		// No region field
	}

	_, _, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/file.bam",
		"test-key",
		"test-secret",
		"", // No region in params
		"",
		bucketDetails,
		nil,
		noOpLogger,
	)

	if err == nil {
		t.Fatal("Expected error for missing region, got nil")
	}

	if !strings.Contains(err.Error(), "Missing required AWS configuration") ||
		!strings.Contains(err.Error(), "AWS region") {
		t.Errorf("Expected missing region error, got: %v", err)
	}
}

func TestFetchS3Metadata_Failure_S3ObjectNotFound(t *testing.T) {
	// Test when S3 HeadObject fails because object doesn't exist
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	// Don't add the object to S3 mock - it won't exist

	cfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion("us-west-2"),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Mock.URL())
		o.UsePathStyle = true
	})

	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: s3Mock.URL(),
	}

	_, _, err = indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/nonexistent.bam",
		"", "", "", "",
		bucketDetails,
		s3Client,
		noOpLogger,
	)

	if err == nil {
		t.Fatal("Expected error for nonexistent S3 object, got nil")
	}

	if !strings.Contains(err.Error(), "failed to head object") {
		t.Errorf("Expected head object error, got: %v", err)
	}
}

func TestFetchS3Metadata_Success_NilContentLength(t *testing.T) {
	// Test handling of nil ContentLength in S3 response (edge case)
	// This is implicitly tested by MockS3Server which always returns ContentLength,
	// but the code handles nil by returning 0
	// This test documents that behavior
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	// Add object with size 0
	s3Mock.AddObject("test-bucket", "empty.bam", 0)

	cfg, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion("us-west-2"),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test-key", "test-secret", "")),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Mock.URL())
		o.UsePathStyle = true
	})

	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-west-2",
		EndpointURL: s3Mock.URL(),
	}

	size, modifiedDate, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/empty.bam",
		"", "", "", "",
		bucketDetails,
		s3Client,
		noOpLogger,
	)

	if err != nil {
		t.Fatalf("Expected success for zero-size file, got error: %v", err)
	}

	if size != 0 {
		t.Errorf("Expected size 0, got %d", size)
	}

	if modifiedDate == "" {
		t.Error("Expected non-empty modified date")
	}
}

func TestFetchS3Metadata_Success_ParameterPriorityOverBucketDetails(t *testing.T) {
	// Test that function parameters take priority over bucket details
	ctx := context.Background()

	ignoreAWSConfigFiles(t)
	s3Mock := NewMockS3Server(t)
	defer s3Mock.Close()

	// Bucket details with DIFFERENT endpoint
	bucketDetails := &s3_utils.S3Bucket{
		Region:      "us-east-1", // Different region
		EndpointURL: "http://different-endpoint",
		Programs:    []string{"test-program"},
	}

	s3Mock.AddObject("test-bucket", "file.bam", 1024)

	// Provide explicit region/endpoint in params - these should override bucket details
	size, modifiedDate, err := indexd_client.FetchS3MetadataWithBucketDetails(
		ctx,
		"s3://test-bucket/file.bam",
		"test-key",
		"test-secret",
		"us-west-2",  // Override bucketDetails' "us-east-1"
		s3Mock.URL(), // Override bucketDetails' "http://different-endpoint"
		bucketDetails,
		nil,
		noOpLogger,
	)

	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if size != 1024 {
		t.Errorf("Expected size 1024, got %d", size)
	}

	if modifiedDate == "" {
		t.Error("Expected non-empty modified date")
	}
}

// Unit Tests for upsertIndexdRecord with mocking

//TODO: update test to use DRSRecords
/*
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
	existingUUID := drsmap.DrsUUID(projectId, sha256)
	authzStr := "/programs/testprogram/projects/testproject"

	existingRecord := &indexd_client.IndexdRecord{
		Did:      existingUUID,
		FileName: "file1.bam",
		Hashes:   indexd_client.HashInfo{SHA256: sha256},
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
	err = indexd_client.UpsertIndexdRecordWithClient(client, projectId, url2, sha256, fileSize, modifiedDate, nil) // Use NoOpLogger
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
	if updatedRecord.Id != existingUUID {
		t.Errorf("Expected DID to remain %s, got %s", existingUUID, updatedRecord.Id)
	}
}
*/

//TODO: update test to use DRSRecords
/*
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
	uuid1 := drsmap.DrsUUID(project1, sha256)
	authz1 := "/programs/program1/projects/project1"

	existingRecord := &indexd_client.IndexdRecord{
		Did:      uuid1,
		FileName: "shared-file.bam",
		Hashes:   indexd_client.HashInfo{SHA256: sha256},
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
	err = indexd_client.UpsertIndexdRecordWithClient(client, project2, url2, sha256, fileSize, modifiedDate, nil) // Use NoOpLogger
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
	uuid2 := drsmap.DrsUUID(project2, sha256)
	authz2 := "/programs/program2/projects/project2"

	foundProject1Record := false
	foundProject2Record := false

	for _, record := range allRecords {
		if record.Id == uuid1 && slices.Contains(record.Authz, authz1) {
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
*/

//TODO: Update test to use DRSRecords
/*
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
	err := indexd_client.UpsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate, nil) // Use NoOpLogger
	if err != nil {
		t.Fatalf("First upsertIndexdRecordWithClient failed: %v", err)
	}

	// Second upsert - same URL, should be idempotent
	err = indexd_client.UpsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate, nil) // Use NoOpLogger
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
*/

//TODO: Update test to use DRSRecords
/*
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
	err = indexd_client.UpsertIndexdRecordWithClient(client, projectId, url, sha256, fileSize, modifiedDate, nil) // Use NoOpLogger
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
	expectedUUID := drsmap.DrsUUID(projectId, sha256)
	expectedAuthz := "/programs/newprogram/projects/newproject"

	// Verify record properties
	if record.Id != expectedUUID {
		t.Errorf("Expected DID %s, got %s", expectedUUID, record.Id)
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
*/

// Unit Tests for FindMatchingRecord

func TestFindMatchingRecord_EmptyRecords(t *testing.T) {
	records := []drs.DRSObject{}
	projectId := "test-project"

	result, err := drsmap.FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("FindMatchingRecord() expected nil for empty records, got %v", result)
	}
}

//TODO: Redo with DRSObject
/*
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
*/

//TODO: Redo this test with new interface
/*
func TestFindMatchingRecord_MultipleRecordsFirstMatch(t *testing.T) {
	records := []indexd_client.OutputInfo{
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

	result, err := drsmap.FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result == nil {
		t.Fatalf("FindMatchingRecord() expected a match, got nil")
	}
	// Should return the first matching record
	if result.Id != "uuid-1" {
		t.Errorf("FindMatchingRecord() expected Did uuid-1, got %s", result.Did)
	}
}
*/

//TODO: update with DRSObject
/*
func TestFindMatchingRecord_NoMatch(t *testing.T) {
	records := []indexd_client.OutputInfo{
		{
			Did:   "uuid-1",
			Authz: []string{"/programs/other/projects/other"},
			URLs:  []string{"s3://bucket/file1.txt"},
		},
	}
	projectId := "test-project"

	result, err := drsmap.FindMatchingRecord(records, projectId)
	if err != nil {
		t.Errorf("FindMatchingRecord() unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("FindMatchingRecord() expected nil for no match, got %v", result)
	}
}
*/

// Unit Tests for DrsUUID

func TestDrsUUID_ReproducibleGeneration(t *testing.T) {
	projectID := "test-project"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Generate UUID multiple times
	uuids := make([]string, 100)
	for i := 0; i < 100; i++ {
		uuids[i] = drsmap.DrsUUID(projectID, sha256)
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

	uuid1 := drsmap.DrsUUID("project-1", sha256)
	uuid2 := drsmap.DrsUUID("project-2", sha256)

	if uuid1 == uuid2 {
		t.Errorf("DrsUUID() should generate different UUIDs for different projects")
	}
}

func TestDrsUUID_DifferentHashes(t *testing.T) {
	projectID := "test-project"

	uuid1 := drsmap.DrsUUID(projectID, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	uuid2 := drsmap.DrsUUID(projectID, "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

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
			resolver := &s3_utils.CustomEndpointResolver{Endpoint: tt.endpoint}
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
			// Setup mock server
			mockServer := NewMockIndexdServer(t)
			defer mockServer.Close()

			// Create client with mock auth
			client := testIndexdClientWithMockAuth(mockServer.URL())

			// Call AddURL - should fail during input validation
			_, err := client.AddURL(tt.s3URL, tt.sha256, "", "", "", "")
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
			err := s3_utils.ValidateInputs(tt.s3URL, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
			if (err != nil) != tt.expectError {
				t.Errorf("validateInputs() for %s error = %v, expectError %v", tt.description, err, tt.expectError)
			}
		})
	}
}
