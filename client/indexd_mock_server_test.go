package client

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calypr/git-drs/drs"
)

//////////////////
// MOCK SERVERS //
//////////////////

// MockIndexdRecord represents a stored Indexd record in memory
type MockIndexdRecord struct {
	Did       string            `json:"did"`
	FileName  string            `json:"file_name"`
	Size      int64             `json:"size"`
	Hashes    map[string]string `json:"hashes"`
	URLs      []string          `json:"urls"`
	Authz     []string          `json:"authz"`
	Metadata  map[string]string `json:"metadata"`
	CreatedAt time.Time         `json:"-"` // Not serialized
}

// MockIndexdServer simulates an Indexd server with in-memory storage
type MockIndexdServer struct {
	httpServer  *httptest.Server
	records     map[string]*MockIndexdRecord
	hashIndex   map[string][]string // hash -> [DIDs]
	recordMutex sync.RWMutex
}

// NewMockIndexdServer creates and starts a mock Indexd server
func NewMockIndexdServer(t *testing.T) *MockIndexdServer {
	mis := &MockIndexdServer{
		records:   make(map[string]*MockIndexdRecord),
		hashIndex: make(map[string][]string),
	}

	mux := http.NewServeMux()

	// Register handlers for /index and /index/ paths
	// /index matches exact path and query params (POST, GET with ?hash=)
	mux.HandleFunc("/index", func(w http.ResponseWriter, r *http.Request) {
		// POST /index - create record
		if r.Method == http.MethodPost {
			mis.handleCreateRecord(w, r)
			return
		}

		// GET /index?hash=... - query by hash
		if r.Method == http.MethodGet {
			mis.handleQueryByHash(w, r)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// /index/index handles /index/index for POST and /index/index?hash= for GET
	mux.HandleFunc("/index/index", func(w http.ResponseWriter, r *http.Request) {
		// POST /index/index - create record
		if r.Method == http.MethodPost {
			mis.handleCreateRecord(w, r)
			return
		}

		// GET /index/index?hash=... - query by hash
		if r.Method == http.MethodGet {
			mis.handleQueryByHash(w, r)
			return
		}

		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	// /ga4gh/drs/v1/objects/ handles GET requests for DRS object and signed URLs
	mux.HandleFunc("/ga4gh/drs/v1/objects/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Extract path after /ga4gh/drs/v1/objects/
		path := strings.TrimPrefix(r.URL.Path, "/ga4gh/drs/v1/objects/")
		if path == "" {
			http.Error(w, "Missing object ID", http.StatusBadRequest)
			return
		}

		// Split path to determine if this is object request or access request
		pathParts := strings.Split(path, "/")

		if len(pathParts) == 1 {
			// GET /ga4gh/drs/v1/objects/{id} - get DRS object
			mis.handleGetDRSObject(w, r, pathParts[0])
		} else if len(pathParts) == 3 && pathParts[1] == "access" {
			// GET /ga4gh/drs/v1/objects/{id}/access/{accessId} - get signed URL
			mis.handleGetSignedURL(w, r, pathParts[0], pathParts[2])
		} else {
			http.Error(w, "Invalid path", http.StatusBadRequest)
		}
	})

	// /index/ matches /index/{guid} (trailing slash pattern)
	mux.HandleFunc("/index/", func(w http.ResponseWriter, r *http.Request) {
		// Extract DID from path: /index/{guid} -> {guid}
		// This handles both /index/{id} and /index/index/{id}
		path := r.URL.Path
		var did string

		if strings.HasPrefix(path, "/index/index/") {
			did = strings.TrimPrefix(path, "/index/index/")
		} else {
			did = strings.TrimPrefix(path, "/index/")
		}

		if did == "" || did == "index" {
			http.Error(w, "Missing DID", http.StatusBadRequest)
			return
		}

		switch r.Method {
		case http.MethodGet:
			mis.handleGetRecord(w, r, did)
		case http.MethodPut:
			mis.handleUpdateRecord(w, r, did)
		case http.MethodDelete:
			mis.handleDeleteRecord(w, r, did)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mis.httpServer = httptest.NewServer(mux)
	return mis
}

func (mis *MockIndexdServer) handleGetRecord(w http.ResponseWriter, r *http.Request, did string) {
	mis.recordMutex.RLock()
	record, exists := mis.records[did]
	mis.recordMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Record not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleGetDRSObject(w http.ResponseWriter, r *http.Request, id string) {
	mis.recordMutex.RLock()
	record, exists := mis.records[id]
	mis.recordMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Object not found"})
		return
	}

	// Convert MockIndexdRecord to DRSObject format
	drsObj := convertMockRecordToDRSObject(record)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(drsObj)
}

func (mis *MockIndexdServer) handleGetSignedURL(w http.ResponseWriter, r *http.Request, objectId, accessId string) {
	mis.recordMutex.RLock()
	_, exists := mis.records[objectId]
	mis.recordMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Object not found"})
		return
	}

	// Create a mock signed URL
	signedURL := drs.AccessURL{
		URL:     fmt.Sprintf("https://signed-url.example.com/%s/%s", objectId, accessId),
		Headers: []string{},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(signedURL)
}

func (mis *MockIndexdServer) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	// Handle IndexdRecordForm (client sends this with POST)
	var form struct {
		IndexdRecord
		Form string `json:"form"`
		Rev  string `json:"rev"`
	}

	if err := json.NewDecoder(r.Body).Decode(&form); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	// Extract the core record data
	record := MockIndexdRecord{
		Did:       form.Did,
		FileName:  form.FileName,
		Size:      form.Size,
		URLs:      form.URLs,
		Authz:     form.Authz,
		Hashes:    convertHashInfoToMap(form.Hashes),
		Metadata:  form.Metadata, // Already map[string]string from IndexdRecord
		CreatedAt: time.Now(),
	}

	if record.Did == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing required field: did"})
		return
	}

	mis.recordMutex.Lock()
	defer mis.recordMutex.Unlock()

	if _, exists := mis.records[record.Did]; exists {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "Record already exists"})
		return
	}

	// Index by hash for queryability
	for hashType, hash := range record.Hashes {
		if hash != "" { // Only index non-empty hashes
			key := hashType + ":" + hash
			mis.hashIndex[key] = append(mis.hashIndex[key], record.Did)
		}
	}

	mis.records[record.Did] = &record

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleUpdateRecord(w http.ResponseWriter, r *http.Request, did string) {
	mis.recordMutex.Lock()
	defer mis.recordMutex.Unlock()

	record, exists := mis.records[did]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Record not found"})
		return
	}

	var update struct {
		URLs []string `json:"urls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	// Append new URLs (avoid duplicates)
	for _, newURL := range update.URLs {
		if !slices.Contains(record.URLs, newURL) {
			record.URLs = append(record.URLs, newURL)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleQueryByHash(w http.ResponseWriter, r *http.Request) {
	hashQuery := r.URL.Query().Get("hash") // format: "sha256:aaaa..."

	mis.recordMutex.RLock()
	dids, exists := mis.hashIndex[hashQuery]
	mis.recordMutex.RUnlock()

	outputRecords := []OutputInfo{}
	if exists {
		mis.recordMutex.RLock()
		for _, did := range dids {
			if record, ok := mis.records[did]; ok {
				// Convert sha256 hash string to HashInfo struct
				hashes := HashInfo{}
				if sha256, ok := record.Hashes["sha256"]; ok {
					hashes.SHA256 = sha256
				}

				// Convert metadata
				metadata := make(map[string]any)
				for k, v := range record.Metadata {
					metadata[k] = v
				}

				outputRecords = append(outputRecords, OutputInfo{
					Did:      record.Did,
					Size:     record.Size,
					Hashes:   hashes,
					URLs:     record.URLs,
					Authz:    record.Authz,
					Metadata: metadata,
				})
			}
		}
		mis.recordMutex.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	// Return wrapped in ListRecords object matching Indexd API
	response := ListRecords{
		Records: outputRecords,
		IDs:     dids,
		Size:    int64(len(outputRecords)),
	}
	json.NewEncoder(w).Encode(response)
}

func (mis *MockIndexdServer) handleDeleteRecord(w http.ResponseWriter, r *http.Request, did string) {
	mis.recordMutex.Lock()
	defer mis.recordMutex.Unlock()

	_, exists := mis.records[did]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	delete(mis.records, did)
	w.WriteHeader(http.StatusNoContent)
}

// URL returns the mock server URL
func (mis *MockIndexdServer) URL() string {
	return mis.httpServer.URL
}

// Close closes the mock server
func (mis *MockIndexdServer) Close() {
	mis.httpServer.Close()
}

// GetAllRecords returns all records for testing purposes
func (mis *MockIndexdServer) GetAllRecords() []*MockIndexdRecord {
	mis.recordMutex.RLock()
	defer mis.recordMutex.RUnlock()

	records := make([]*MockIndexdRecord, 0, len(mis.records))
	for _, record := range mis.records {
		records = append(records, record)
	}
	return records
}

// GetRecord retrieves a single record by DID
func (mis *MockIndexdServer) GetRecord(did string) *MockIndexdRecord {
	mis.recordMutex.RLock()
	defer mis.recordMutex.RUnlock()
	return mis.records[did]
}

// MockGen3Server simulates Gen3 /user/data/buckets endpoint
type MockGen3Server struct {
	httpServer *httptest.Server
	s3Endpoint string
}

// NewMockGen3Server creates and starts a mock Gen3 server
func NewMockGen3Server(t *testing.T, s3Endpoint string) *MockGen3Server {
	mgs := &MockGen3Server{
		s3Endpoint: s3Endpoint,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/user/data/buckets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := map[string]interface{}{
			"S3_BUCKETS": map[string]interface{}{
				"test-bucket": map[string]interface{}{
					"region":       "us-west-2",
					"endpoint_url": mgs.s3Endpoint,
					"programs":     []string{"test-program"},
				},
			},
			"GS_BUCKETS": map[string]interface{}{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	mgs.httpServer = httptest.NewServer(mux)
	return mgs
}

// URL returns the mock server URL
func (mgs *MockGen3Server) URL() string {
	return mgs.httpServer.URL
}

// Client returns the mock server HTTP client
func (mgs *MockGen3Server) Client() *http.Client {
	return mgs.httpServer.Client()
}

// Close closes the mock server
func (mgs *MockGen3Server) Close() {
	mgs.httpServer.Close()
}

// MockS3Object represents a stored S3 object
type MockS3Object struct {
	Size         int64
	LastModified time.Time
	ContentType  string
}

// MockS3Server simulates S3 HEAD object endpoint
type MockS3Server struct {
	httpServer *httptest.Server
	objects    map[string]*MockS3Object // "bucket/key" -> object
	objMutex   sync.RWMutex
}

// ignoreAWSConfigFiles is a helper function to prevent reading from the real AWS config files
func ignoreAWSConfigFiles(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
}

// NewMockS3Server creates and starts a mock S3 server
func NewMockS3Server(t *testing.T) *MockS3Server {
	mss := &MockS3Server{
		objects: make(map[string]*MockS3Object),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		if r.Method == http.MethodHead || r.Method == http.MethodGet {
			mss.handleHeadObject(w, r, path)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mss.httpServer = httptest.NewServer(mux)
	return mss
}

func (mss *MockS3Server) handleHeadObject(w http.ResponseWriter, r *http.Request, path string) {
	mss.objMutex.RLock()
	object, exists := mss.objects[path]
	mss.objMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprintf("%d", object.Size))
	w.Header().Set("Last-Modified", object.LastModified.UTC().Format(http.TimeFormat))
	w.Header().Set("Content-Type", object.ContentType)
	w.Header().Set("ETag", fmt.Sprintf("\"%x\"", object.LastModified.Unix()))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, 0))
	}
}

// AddObject adds a mock S3 object for testing
func (mss *MockS3Server) AddObject(bucket, key string, size int64) {
	path := bucket + "/" + key
	mss.objMutex.Lock()
	defer mss.objMutex.Unlock()

	mss.objects[path] = &MockS3Object{
		Size:         size,
		LastModified: time.Now(),
		ContentType:  "application/octet-stream",
	}
}

// URL returns the mock server URL
func (mss *MockS3Server) URL() string {
	return mss.httpServer.URL
}

// Close closes the mock server
func (mss *MockS3Server) Close() {
	mss.httpServer.Close()
}

// Helper functions for type conversion

// convertHashInfoToMap converts HashInfo struct to map[string]string
func convertHashInfoToMap(hashes HashInfo) map[string]string {
	result := make(map[string]string)
	if hashes.MD5 != "" {
		result["md5"] = hashes.MD5
	}
	if hashes.SHA != "" {
		result["sha"] = hashes.SHA
	}
	if hashes.SHA256 != "" {
		result["sha256"] = hashes.SHA256
	}
	if hashes.SHA512 != "" {
		result["sha512"] = hashes.SHA512
	}
	if hashes.CRC != "" {
		result["crc"] = hashes.CRC
	}
	if hashes.ETag != "" {
		result["etag"] = hashes.ETag
	}
	return result
}

// convertMapAnyToString converts map[string]any to map[string]string
func convertMapAnyToString(input map[string]any) map[string]string {
	result := make(map[string]string)
	for k, v := range input {
		if str, ok := v.(string); ok {
			result[k] = str
		}
	}
	return result
}

// convertMockRecordToDRSObject converts a MockIndexdRecord to a DRS object
func convertMockRecordToDRSObject(record *MockIndexdRecord) *drs.DRSObject {
	// Convert hashes to Checksum array
	checksums := make([]drs.Checksum, 0)
	for hashType, hashValue := range record.Hashes {
		if hashValue != "" {
			checksums = append(checksums, drs.Checksum{
				Type:     drs.ChecksumType(hashType),
				Checksum: hashValue,
			})
		}
	}

	// Convert URLs to AccessMethods
	accessMethods := make([]drs.AccessMethod, 0)
	for i, url := range record.URLs {
		accessMethods = append(accessMethods, drs.AccessMethod{
			Type:     "https",
			AccessID: fmt.Sprintf("access-method-%d", i),
			AccessURL: drs.AccessURL{
				URL:     url,
				Headers: []string{},
			},
		})
	}

	return &drs.DRSObject{
		Id:            record.Did,
		Name:          record.FileName,
		Size:          record.Size,
		Checksums:     checksums,
		AccessMethods: accessMethods,
		CreatedTime:   record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Description:   "DRS object created from Indexd record",
	}
}
