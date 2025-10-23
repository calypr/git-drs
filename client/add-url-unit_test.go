package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// Unit Tests for validateInputs
// (Already covered in add-url_test.go, but adding edge cases)

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

// Unit Tests for getBucketDetails

func TestGetBucketDetails_MockServer(t *testing.T) {
	// Skip this test as it requires config setup
	// This would be better as an integration test
	t.Skip("Requires config setup - moved to integration tests")
}

func TestAddURL_InvalidS3URLFormat(t *testing.T) {
	// Test input validation through public API (no nil parameters needed!)
	tests := []struct {
		name      string
		s3URL     string
		sha256    string
		wantError bool
	}{
		{"invalid S3 URL - no prefix", "bucket/file.txt", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"invalid S3 URL - http", "http://bucket/file.txt", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
		{"invalid SHA256 - too short", "s3://bucket/file.txt", "e3b0c44298fc1c149afbf4c8996fb92427ae41e464", true},
		{"invalid SHA256 - non-hex", "s3://bucket/file.txt", "z3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := AddURL(tt.s3URL, tt.sha256, "key", "secret", "us-west-2", "https://s3.amazonaws.com")
			if tt.wantError && err == nil {
				t.Errorf("Expected error for %s, got nil", tt.name)
			}
		})
	}
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

// Unit Tests for HTTP handling

func TestHTTPRequestWithMockServer(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/data/buckets" {
			response := S3BucketsResponse{
				S3Buckets: map[string]S3Bucket{
					"test-bucket": {
						Region:      "us-west-2",
						EndpointURL: "https://s3.amazonaws.com",
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Test the request
	resp, err := http.Get(server.URL + "/user/data/buckets")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var response S3BucketsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if bucket, exists := response.S3Buckets["test-bucket"]; !exists {
		t.Errorf("Expected test-bucket in response")
	} else if bucket.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", bucket.Region)
	}
}

func TestHTTPRequest_ErrorHandling(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		responseBody string
		expectError  bool
	}{
		{
			name:         "404 Not Found",
			statusCode:   http.StatusNotFound,
			responseBody: `{"error": "not found"}`,
			expectError:  true,
		},
		{
			name:         "500 Internal Server Error",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{"error": "internal error"}`,
			expectError:  true,
		},
		{
			name:         "401 Unauthorized",
			statusCode:   http.StatusUnauthorized,
			responseBody: `{"error": "unauthorized"}`,
			expectError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			resp, err := http.Get(server.URL)
			if err != nil {
				t.Fatalf("HTTP GET failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK && tt.expectError {
				t.Errorf("Expected error status, got 200 OK")
			}

			if resp.StatusCode != http.StatusOK && !tt.expectError {
				t.Errorf("Expected success status, got %d", resp.StatusCode)
			}
		})
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

// Benchmark Tests
func BenchmarkFindMatchingRecord(b *testing.B) {
	// Setup test data
	records := make([]OutputInfo, 100)
	for i := 0; i < 100; i++ {
		records[i] = OutputInfo{
			Did:   fmt.Sprintf("uuid-%d", i),
			Authz: []string{fmt.Sprintf("/programs/program-%d/projects/project-%d", i, i)},
		}
	}
	// Add matching record in the middle
	records[50].Authz = []string{"/programs/test/projects/project"}
	projectId := "test-project"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = FindMatchingRecord(records, projectId)
	}
}

// Table-driven test for S3 URL parsing edge cases
=
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

// Test time formatting and parsing

func TestTimeFormatting(t *testing.T) {
	// Test RFC3339 time formatting used in modified dates
	testTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	formatted := testTime.Format(time.RFC3339)

	if formatted != "2024-01-01T12:00:00Z" {
		t.Errorf("Expected time format 2024-01-01T12:00:00Z, got %s", formatted)
	}

	// Test parsing back
	parsed, err := time.Parse(time.RFC3339, formatted)
	if err != nil {
		t.Errorf("Failed to parse time: %v", err)
	}

	if !parsed.Equal(testTime) {
		t.Errorf("Parsed time doesn't match original: expected %v, got %v", testTime, parsed)
	}
}

// Test AWS pointer helpers (from aws-sdk-go-v2)

func TestAWSPointerHelpers(t *testing.T) {
	// Test that AWS String pointer works
	str := "test-string"
	ptr := aws.String(str)

	if ptr == nil {
		t.Fatal("aws.String() returned nil")
	}

	if *ptr != str {
		t.Errorf("Expected %s, got %s", str, *ptr)
	}

	// Test with empty string
	emptyPtr := aws.String("")
	if emptyPtr == nil {
		t.Fatal("aws.String() returned nil for empty string")
	}

	if *emptyPtr != "" {
		t.Errorf("Expected empty string, got %s", *emptyPtr)
	}
}

// Test error wrapping and messages
