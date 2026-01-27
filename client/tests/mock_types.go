package indexd_tests

import (
	"net/http"
	"sync"
)

// MockIndexdRecord represents a test record in the mock server
type MockIndexdRecord struct {
	Did      string
	FileName string
	Size     int64
	Hashes   map[string]string
	URLs     []string
	Authz    []string
}

// MockIndexdServer is a mock server for testing
type MockIndexdServer struct {
	records     map[string]*MockIndexdRecord
	hashIndex   map[string][]string
	recordMutex sync.RWMutex
	server      *http.Server
}

// MockAuthHandler is a mock authentication handler for testing
type MockAuthHandler struct{}

func (m *MockAuthHandler) RefreshAccessToken() (string, error) {
	return "mock-token", nil
}

func (m *MockAuthHandler) GetAccessToken() string {
	return "mock-token"
}
