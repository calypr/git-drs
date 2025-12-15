package indexd_tests

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/calypr/data-client/client/jwt"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drslog"
)

//////////////////////////
// TEST CONSTANTS       //
//////////////////////////

const (
	testSHA256Hash    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testDefaultBucket = "test-bucket"
	testDefaultAuthz  = "/programs/test/projects/project"
)

//////////////////////////
// MOCK AUTH HANDLERS   //
//////////////////////////

// MockAuthHandler implements AuthHandler for testing without Gen3 credentials
// It simply sets a Bearer token header without making any external calls
type MockAuthHandler struct{}

func (m *MockAuthHandler) AddAuthHeader(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer mock-test-token")
	return nil
}

// testErrorAuthHandler is a test helper for testing auth errors
type testErrorAuthHandler struct {
	Cred jwt.Credential
	err  error
}

func (e *testErrorAuthHandler) AddAuthHeader(req *http.Request) error {
	return e.err
}

//////////////////////////
// CLIENT BUILDERS      //
//////////////////////////

// testIndexdClient creates an IndexdClient pointing to a mock server with real auth handler
func testIndexdClient(baseURL string) *indexd_client.IndexDClient {
	url, _ := url.Parse(baseURL)
	return &indexd_client.IndexDClient{
		Base:        url,
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      drslog.GetLogger(),
		AuthHandler: &indexd_client.RealAuthHandler{Cred: jwt.Credential{Profile: "test-remote"}},
	}
}

// testIndexdClientWithMockAuth creates an IndexdClient with mocked authentication for testing
// This helper enables testing client methods without requiring Gen3 credentials or config files
func testIndexdClientWithMockAuth(baseURL string) *indexd_client.IndexDClient {
	url, _ := url.Parse(baseURL)
	return &indexd_client.IndexDClient{
		Base:        url,
		ProjectId:   "test-project",
		BucketName:  "test-bucket",
		Logger:      drslog.GetLogger(),
		AuthHandler: &MockAuthHandler{},
	}
}

//////////////////////////
// TEST RECORD BUILDERS //
//////////////////////////

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

//////////////////////////
// MOCK SERVER HELPERS  //
//////////////////////////

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

//////////////////////////
// UTILITY HELPERS      //
//////////////////////////

// parseURL is a helper function to parse URL
func parseURL(rawURL string) *url.URL {
	u, _ := url.Parse(rawURL)
	return u
}

// ignoreAWSConfigFiles is a helper function to prevent reading from the real AWS config files
// Used in tests that create AWS clients to avoid environment interference
func ignoreAWSConfigFiles(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
}
