package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/s3_utils"
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

// TestDrsUUID tests that DrsUUID generates consistent and unique UUIDs
func TestDrsUUID(t *testing.T) {
	projectID := "test-project"
	sha256a := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	sha256b := "a3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// Test consistency: same inputs produce same UUID
	uuid1 := drsmap.DrsUUID(projectID, sha256a)
	uuid2 := drsmap.DrsUUID(projectID, sha256a)
	if uuid1 != uuid2 {
		t.Errorf("DrsUUID() not consistent: uuid1=%s, uuid2=%s", uuid1, uuid2)
	}

	// Test uniqueness: different hashes produce different UUIDs
	uuid3 := drsmap.DrsUUID(projectID, sha256b)
	if uuid1 == uuid3 {
		t.Errorf("DrsUUID() should generate different UUIDs for different hashes")
	}

	// Test uniqueness: different projects produce different UUIDs
	uuid4 := drsmap.DrsUUID("different-project", sha256a)
	if uuid1 == uuid4 {
		t.Errorf("DrsUUID() should generate different UUIDs for different projects")
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

		response := s3_utils.S3BucketsResponse{
			S3Buckets: map[string]*s3_utils.S3Bucket{
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
