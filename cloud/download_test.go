package cloud

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadSignedUrl(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	dst := filepath.Join(t.TempDir(), "file.txt")
	if err := DownloadSignedUrl(server.URL, dst); err != nil {
		t.Fatalf("DownloadSignedUrl error: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("unexpected file content: %s", string(data))
	}
}

func TestDownloadSignedUrl_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad"))
	}))
	defer server.Close()

	dst := filepath.Join(t.TempDir(), "file.txt")
	if err := DownloadSignedUrl(server.URL, dst); err == nil {
		t.Fatalf("expected error")
	}
}
