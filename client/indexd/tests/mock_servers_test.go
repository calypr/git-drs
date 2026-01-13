package indexd_tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/sonic/decoder"
	"github.com/bytedance/sonic/encoder"
	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
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
	httpServer     *httptest.Server
	records        map[string]*MockIndexdRecord
	hashIndex      map[string][]string // hash -> [DIDs]
	signedURLBase  string
	hashQueryCount int
	recordMutex    sync.RWMutex
}

// NewMockIndexdServer creates and starts a mock Indexd server
func NewMockIndexdServer(t *testing.T) *MockIndexdServer {
	mis := &MockIndexdServer{
		records:       make(map[string]*MockIndexdRecord),
		hashIndex:     make(map[string][]string),
		signedURLBase: "https://signed-url.example.com",
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

	mux.HandleFunc("/signed/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Record not found"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encoder.NewStreamEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleGetDRSObject(w http.ResponseWriter, r *http.Request, id string) {
	mis.recordMutex.RLock()
	record, exists := mis.records[id]
	mis.recordMutex.RUnlock()
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Object not found"})
		return
	}

	// Build standard DRS checksums array
	checksums := []map[string]string{}
	for typ, sum := range record.Hashes {
		if sum != "" {
			checksums = append(checksums, map[string]string{
				"type":     strings.ToLower(typ),
				"checksum": sum,
			})
		}
	}

	// Build access methods
	accessMethods := []map[string]any{}
	for i, url := range record.URLs {
		am := map[string]any{
			"type":       "https",
			"access_id":  fmt.Sprintf("https-%d", i),
			"access_url": map[string]string{"url": url},
		}
		// Only add authorizations if present, and as a SINGLE object (not array)
		if len(record.Authz) > 0 {
			am["authorizations"] = map[string]string{
				"value": record.Authz[0],
			}
		}
		accessMethods = append(accessMethods, am)
	}

	// Full response
	response := map[string]any{
		"id":             record.Did,
		"name":           record.FileName,
		"size":           record.Size,
		"created_time":   record.CreatedAt.Format(time.RFC3339),
		"checksums":      checksums,
		"access_methods": accessMethods,
		"description":    "Mock DRS object from Indexd record",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	encoder.NewStreamEncoder(w).Encode(response)
}

func (mis *MockIndexdServer) handleGetSignedURL(w http.ResponseWriter, r *http.Request, objectId, accessId string) {
	mis.recordMutex.RLock()
	_, exists := mis.records[objectId]
	mis.recordMutex.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Object not found"})
		return
	}

	// Create a mock signed URL
	base := strings.TrimSuffix(mis.signedURLBase, "/")
	signedURL := drs.AccessURL{
		URL:     fmt.Sprintf("%s/%s/%s", base, objectId, accessId),
		Headers: []string{},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	encoder.NewStreamEncoder(w).Encode(signedURL)
}

func (mis *MockIndexdServer) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	// Handle IndexdRecordForm (client sends this with POST)
	var form struct {
		indexd_client.IndexdRecord
		Form string `json:"form"`
		Rev  string `json:"rev"`
	}

	if err := decoder.NewStreamDecoder(r.Body).Decode(&form); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	// Extract the core record data
	record := MockIndexdRecord{
		Did:       form.Did,
		FileName:  form.FileName,
		Size:      form.Size,
		URLs:      form.URLs,
		Authz:     form.Authz,
		Hashes:    hash.ConvertHashInfoToMap(form.Hashes),
		Metadata:  form.Metadata, // Already map[string]string from IndexdRecord
		CreatedAt: time.Now(),
	}

	if record.Did == "" {
		w.WriteHeader(http.StatusBadRequest)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Missing required field: did"})
		return
	}

	mis.recordMutex.Lock()
	defer mis.recordMutex.Unlock()

	if _, exists := mis.records[record.Did]; exists {
		w.WriteHeader(http.StatusConflict)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Record already exists"})
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
	encoder.NewStreamEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleUpdateRecord(w http.ResponseWriter, r *http.Request, did string) {
	mis.recordMutex.Lock()
	defer mis.recordMutex.Unlock()

	record, exists := mis.records[did]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Record not found"})
		return
	}

	var update struct {
		URLs []string `json:"urls"`
	}
	if err := decoder.NewStreamDecoder(r.Body).Decode(&update); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encoder.NewStreamEncoder(w).Encode(map[string]string{"error": "Invalid JSON"})
		return
	}

	// Append new URLs (avoid duplicates)
	for _, newURL := range update.URLs {
		if !slices.Contains(record.URLs, newURL) {
			record.URLs = append(record.URLs, newURL)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	encoder.NewStreamEncoder(w).Encode(record)
}

func (mis *MockIndexdServer) handleQueryByHash(w http.ResponseWriter, r *http.Request) {
	hashQuery := r.URL.Query().Get("hash") // format: "sha256:aaaa..."

	mis.recordMutex.Lock()
	mis.hashQueryCount++
	mis.recordMutex.Unlock()

	mis.recordMutex.RLock()
	dids, exists := mis.hashIndex[hashQuery]
	mis.recordMutex.RUnlock()

	outputRecords := []indexd_client.OutputInfo{}
	if exists {
		mis.recordMutex.RLock()
		for _, did := range dids {
			if record, ok := mis.records[did]; ok {
				// Convert sha256 hash string to HashInfo struct
				hashes := hash.HashInfo{}
				if sha256, ok := record.Hashes["sha256"]; ok {
					hashes.SHA256 = sha256
				}

				// Convert metadata
				metadata := make(map[string]any)
				for k, v := range record.Metadata {
					metadata[k] = v
				}

				outputRecords = append(outputRecords, indexd_client.OutputInfo{
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
	response := indexd_client.ListRecords{
		Records: outputRecords,
		IDs:     dids,
		Size:    int64(len(outputRecords)),
	}
	encoder.NewStreamEncoder(w).Encode(response)
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

// HashQueryCount returns the number of hash query requests observed by the mock server.
func (mis *MockIndexdServer) HashQueryCount() int {
	mis.recordMutex.RLock()
	defer mis.recordMutex.RUnlock()
	return mis.hashQueryCount
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

		response := map[string]any{
			"S3_BUCKETS": map[string]any{
				"test-bucket": map[string]any{
					"region":       "us-west-2",
					"endpoint_url": mgs.s3Endpoint,
					"programs":     []string{"test-program"},
				},
			},
			"GS_BUCKETS": map[string]any{},
		}

		w.Header().Set("Content-Type", "application/json")
		encoder.NewStreamEncoder(w).Encode(response)
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
func convertMockRecordToDRSObject(record *MockIndexdRecord) *drs.DRSObject {

	// Convert URLs to AccessMethods
	accessMethods := make([]drs.AccessMethod, 0)
	for i, url := range record.URLs {
		// Get the first authz as the authorization for this access method
		var authzPtr *drs.Authorizations
		if len(record.Authz) > 0 {
			authzPtr = &drs.Authorizations{
				Value: record.Authz[0],
			}
		}

		accessMethods = append(accessMethods, drs.AccessMethod{
			Type:     "https",
			AccessID: fmt.Sprintf("access-method-%d", i),
			AccessURL: drs.AccessURL{
				URL:     url,
				Headers: []string{},
			},
			Authorizations: authzPtr,
		})
	}

	return &drs.DRSObject{
		Id:            record.Did,
		Name:          record.FileName,
		Size:          record.Size,
		Checksums:     hash.ConvertStringMapToHashInfo(record.Hashes),
		AccessMethods: accessMethods,
		CreatedTime:   record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Description:   "DRS object created from Indexd record",
	}
}
