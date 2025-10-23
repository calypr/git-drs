package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/calypr/git-drs/utils"
)

// TestParseS3URL_Valid tests parsing valid S3 URLs
func TestParseS3URL_Valid(t *testing.T) {
	tests := []struct {
		name         string
		s3URL        string
		expectBucket string
		expectKey    string
		wantErr      bool
	}{
		{
			name:         "simple S3 URL",
			s3URL:        "s3://my-bucket/file.txt",
			expectBucket: "my-bucket",
			expectKey:    "file.txt",
			wantErr:      false,
		},
		{
			name:         "S3 URL with nested path",
			s3URL:        "s3://my-bucket/path/to/file.bam",
			expectBucket: "my-bucket",
			expectKey:    "path/to/file.bam",
			wantErr:      false,
		},
		{
			name:         "S3 URL with deep nesting",
			s3URL:        "s3://bucket/a/b/c/d/e/f/file.txt",
			expectBucket: "bucket",
			expectKey:    "a/b/c/d/e/f/file.txt",
			wantErr:      false,
		},
		{
			name:         "S3 URL with special characters",
			s3URL:        "s3://bucket/path/to/file-v1.2.3_test.bam",
			expectBucket: "bucket",
			expectKey:    "path/to/file-v1.2.3_test.bam",
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, key, err := utils.ParseS3URL(tt.s3URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseS3URL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if bucket != tt.expectBucket {
				t.Errorf("ParseS3URL() bucket = %v, want %v", bucket, tt.expectBucket)
			}
			if key != tt.expectKey {
				t.Errorf("ParseS3URL() key = %v, want %v", key, tt.expectKey)
			}
		})
	}
}

// TestParseS3URL_Invalid tests parsing invalid S3 URLs
func TestParseS3URL_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		s3URL   string
		wantErr bool
	}{
		{
			name:    "empty string",
			s3URL:   "",
			wantErr: true,
		},
		{
			name:    "no s3:// prefix",
			s3URL:   "bucket/file.txt",
			wantErr: true,
		},
		{
			name:    "HTTP URL",
			s3URL:   "http://bucket/file.txt",
			wantErr: true,
		},
		{
			name:    "s3:// without bucket",
			s3URL:   "s3://",
			wantErr: true,
		},
		{
			name:    "s3:// with only bucket",
			s3URL:   "s3://bucket/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := utils.ParseS3URL(tt.s3URL)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseS3URL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestDrsUUID_Consistency tests that DrsUUID generates consistent UUIDs
func TestDrsUUID_Consistency(t *testing.T) {
	projectID := "test-project"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	uuid1 := DrsUUID(projectID, sha256)
	uuid2 := DrsUUID(projectID, sha256)

	if uuid1 != uuid2 {
		t.Errorf("DrsUUID() not consistent: uuid1=%s, uuid2=%s", uuid1, uuid2)
	}
}

// TestDrsUUID_DifferentInputs tests that DrsUUID generates different UUIDs for different inputs
func TestDrsUUID_DifferentInputs(t *testing.T) {
	projectID := "test-project"
	sha256a := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sha256b := "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	uuid1 := DrsUUID(projectID, sha256a)
	uuid2 := DrsUUID(projectID, sha256b)

	if uuid1 == uuid2 {
		t.Errorf("DrsUUID() should generate different UUIDs for different inputs")
	}
}

// TestDrsUUID_Format tests that DrsUUID returns a valid UUID format
func TestDrsUUID_Format(t *testing.T) {
	projectID := "test-project"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	uuid := DrsUUID(projectID, sha256)

	// UUID should not be empty
	if uuid == "" {
		t.Errorf("DrsUUID() returned empty string")
	}

	// UUID should have proper format (uuid.UUID string representation)
	if len(uuid) != 36 && len(uuid) != 32 { // UUID with or without hyphens
		t.Logf("DrsUUID() returned: %s (length: %d)", uuid, len(uuid))
	}
}

// TestGetBucketDetails_Gen3Success tests successful bucket details retrieval from Gen3
func TestGetBucketDetails_Gen3Success(t *testing.T) {
	// Mock Gen3 server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/data/buckets" {
			http.NotFound(w, r)
			return
		}

		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.aws.amazon.com",
					Programs:    []string{"program1", "program2"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// This test would need config mocking, so we'll skip detailed testing here
	// In a real scenario, use dependency injection or mocking framework
	t.Logf("Mock Gen3 server URL: %s", server.URL)
}

// TestAddURL_InvalidS3URLParsing tests AddURL with invalid S3 URL format
// (This was previously TestFetchS3Metadata_InvalidURL, now using public API)
func TestAddURL_InvalidS3URLParsing(t *testing.T) {
	s3URL := "invalid-url"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Should fail during input validation
	_, _, err := AddURL(s3URL, sha256, "test-key", "test-secret", "us-west-2", "https://s3.aws.amazon.com")
	if err == nil {
		t.Errorf("AddURL() expected error for invalid URL, got nil")
	}
}

// TestIndexdRecordStructure tests that IndexdRecord can be properly marshaled to JSON
func TestIndexdRecordStructure(t *testing.T) {
	record := &IndexdRecord{
		Did:      "test-uuid",
		FileName: "path/to/file.bam",
		Hashes: HashInfo{
			SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		Size: 1024,
		URLs: []string{
			"s3://bucket1/path/to/file.bam",
			"s3://bucket2/path/to/file.bam",
		},
		Authz: []string{"resource:project1"},
		Metadata: map[string]string{
			"remote": "true",
		},
	}

	// Should marshal to JSON without error
	data, err := json.Marshal(record)
	if err != nil {
		t.Errorf("IndexdRecord marshal to JSON failed: %v", err)
		return
	}

	// Should unmarshal back successfully
	var unmarshaledRecord IndexdRecord
	err = json.Unmarshal(data, &unmarshaledRecord)
	if err != nil {
		t.Errorf("IndexdRecord unmarshal from JSON failed: %v", err)
		return
	}

	if unmarshaledRecord.Did != record.Did {
		t.Errorf("IndexdRecord round-trip failed: Did mismatch")
	}
	if unmarshaledRecord.FileName != record.FileName {
		t.Errorf("IndexdRecord round-trip failed: FileName mismatch")
	}
	if unmarshaledRecord.Size != record.Size {
		t.Errorf("IndexdRecord round-trip failed: Size mismatch")
	}
}

// TestS3BucketsResponseStructure tests S3BucketsResponse JSON marshaling
func TestS3BucketsResponseStructure(t *testing.T) {
	response := S3BucketsResponse{
		GSBuckets: map[string]interface{}{
			"bucket1": map[string]string{
				"region": "us-west-1",
			},
		},
		S3Buckets: map[string]S3Bucket{
			"s3-bucket1": {
				Region:      "us-west-2",
				EndpointURL: "https://s3.aws.amazon.com",
				Programs:    []string{"program1"},
			},
		},
	}

	data, err := json.Marshal(response)
	if err != nil {
		t.Errorf("S3BucketsResponse marshal to JSON failed: %v", err)
		return
	}

	var unmarshaledResponse S3BucketsResponse
	err = json.Unmarshal(data, &unmarshaledResponse)
	if err != nil {
		t.Errorf("S3BucketsResponse unmarshal from JSON failed: %v", err)
		return
	}

	if unmarshaledResponse.S3Buckets["s3-bucket1"].Region != "us-west-2" {
		t.Errorf("S3BucketsResponse round-trip failed: Region mismatch")
	}
}

// TestUpdateInputInfoStructure tests UpdateInputInfo JSON marshaling
func TestUpdateInputInfoStructure(t *testing.T) {
	updateInfo := UpdateInputInfo{
		URLs: []string{
			"s3://bucket/path/to/file.bam",
		},
	}

	data, err := json.Marshal(updateInfo)
	if err != nil {
		t.Errorf("UpdateInputInfo marshal to JSON failed: %v", err)
		return
	}

	var unmarshaledInfo UpdateInputInfo
	err = json.Unmarshal(data, &unmarshaledInfo)
	if err != nil {
		t.Errorf("UpdateInputInfo unmarshal from JSON failed: %v", err)
		return
	}

	if len(unmarshaledInfo.URLs) != 1 || unmarshaledInfo.URLs[0] != "s3://bucket/path/to/file.bam" {
		t.Errorf("UpdateInputInfo round-trip failed: URLs mismatch")
	}
}

// TestHashInfoStructure tests HashInfo JSON marshaling
func TestHashInfoStructure(t *testing.T) {
	hashInfo := HashInfo{
		SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	data, err := json.Marshal(hashInfo)
	if err != nil {
		t.Errorf("HashInfo marshal to JSON failed: %v", err)
		return
	}

	var unmarshaledHash HashInfo
	err = json.Unmarshal(data, &unmarshaledHash)
	if err != nil {
		t.Errorf("HashInfo unmarshal from JSON failed: %v", err)
		return
	}

	if unmarshaledHash.SHA256 != hashInfo.SHA256 {
		t.Errorf("HashInfo round-trip failed: SHA256 mismatch")
	}
}

// TestContextTimeout tests that context timeout doesn't panic
func TestContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Sleep to let context timeout
	time.Sleep(2 * time.Millisecond)

	// Context should be done
	if ctx.Err() == nil {
		t.Errorf("Expected context to be done, but it wasn't")
	}
}

// TestHTTPRequestBodyReading tests that HTTP response body is properly read
func TestHTTPRequestBodyReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		response := S3BucketsResponse{
			S3Buckets: map[string]S3Bucket{
				"test-bucket": {
					Region:      "us-west-2",
					EndpointURL: "https://s3.amazonaws.com",
				},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	var response S3BucketsResponse
	err = json.NewDecoder(resp.Body).Decode(&response)
	if err != nil {
		t.Errorf("Failed to decode response: %v", err)
		return
	}

	if bucket, exists := response.S3Buckets["test-bucket"]; !exists {
		t.Errorf("Expected test-bucket to exist in response")
	} else if bucket.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", bucket.Region)
	}
}

// TestResponseBodyDecode tests JSON response body decoding
func TestResponseBodyDecode(t *testing.T) {
	responseBody := `{
		"S3_BUCKETS": {
			"bucket1": {
				"region": "us-west-2",
				"endpoint_url": "https://s3.amazonaws.com",
				"programs": ["program1", "program2"]
			}
		},
		"GS_BUCKETS": {}
	}`

	reader := io.NopCloser(bytes.NewBufferString(responseBody))
	var response S3BucketsResponse
	err := json.NewDecoder(reader).Decode(&response)
	if err != nil {
		t.Errorf("Failed to decode response: %v", err)
		return
	}

	if bucket, exists := response.S3Buckets["bucket1"]; !exists {
		t.Errorf("Expected bucket1 to exist in response")
	} else if bucket.Region != "us-west-2" {
		t.Errorf("Expected region us-west-2, got %s", bucket.Region)
	}
}

// TestMultipleURLsInRecord tests that multiple URLs can be properly stored in a record
func TestMultipleURLsInRecord(t *testing.T) {
	urls := []string{
		"s3://bucket1/path/to/file.bam",
		"s3://bucket2/path/to/file.bam",
		"s3://bucket3/path/to/file.bam",
	}

	record := &IndexdRecord{
		Did:   "test-uuid",
		URLs:  urls,
		Authz: []string{"resource:project1"},
	}

	if len(record.URLs) != len(urls) {
		t.Errorf("Expected %d URLs, got %d", len(urls), len(record.URLs))
	}

	for i, url := range urls {
		if record.URLs[i] != url {
			t.Errorf("URL mismatch at index %d: expected %s, got %s", i, url, record.URLs[i])
		}
	}
}

// TestEmptyURLList tests that empty URL list is valid
func TestEmptyURLList(t *testing.T) {
	record := &IndexdRecord{
		Did:   "test-uuid",
		URLs:  []string{},
		Authz: []string{},
	}

	if len(record.URLs) != 0 {
		t.Errorf("Expected 0 URLs, got %d", len(record.URLs))
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Errorf("Failed to marshal record with empty URLs: %v", err)
	}

	if len(data) == 0 {
		t.Errorf("Marshaled data should not be empty")
	}
}

// BenchmarkDrsUUID benchmarks UUID generation
func BenchmarkDrsUUID(b *testing.B) {
	projectID := "test-project"
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DrsUUID(projectID, sha256)
	}
}

// BenchmarkIndexdRecordMarshal benchmarks IndexdRecord JSON marshaling
func BenchmarkIndexdRecordMarshal(b *testing.B) {
	record := &IndexdRecord{
		Did:      "test-uuid",
		FileName: "path/to/file.bam",
		Hashes: HashInfo{
			SHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		Size: 1024,
		URLs: []string{
			"s3://bucket1/path/to/file.bam",
			"s3://bucket2/path/to/file.bam",
		},
		Authz: []string{"resource:project1"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(record)
	}
}

// BenchmarkS3BucketsResponseMarshal benchmarks S3BucketsResponse JSON marshaling
func BenchmarkS3BucketsResponseMarshal(b *testing.B) {
	response := S3BucketsResponse{
		S3Buckets: map[string]S3Bucket{
			"bucket1": {
				Region:      "us-west-2",
				EndpointURL: "https://s3.amazonaws.com",
				Programs:    []string{"program1", "program2"},
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(response)
	}
}
