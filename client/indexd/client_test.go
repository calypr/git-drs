package indexd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/indexd"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/data-client/logs"
)

type mockIndexdServer struct {
	mu                sync.Mutex
	listProjectPages  int
	listObjectsPages  int
	lastUpdatePayload indexd.UpdateInputInfo
}

func (m *mockIndexdServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/index/index":
			if hashQuery := r.URL.Query().Get("hash"); hashQuery != "" {
				record := sampleDataClientOutputInfo()
				page := indexd.ListRecords{Records: []indexd.OutputInfo{record}}
				w.WriteHeader(http.StatusOK)
				_ = encoder.NewStreamEncoder(w).Encode(page)
				return
			}
			if r.URL.Query().Get("authz") != "" {
				m.mu.Lock()
				page := m.listProjectPages
				m.listProjectPages++
				m.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				if page == 0 {
					_ = encoder.NewStreamEncoder(w).Encode(indexd.ListRecords{Records: []indexd.OutputInfo{sampleDataClientOutputInfo()}})
				} else {
					_ = encoder.NewStreamEncoder(w).Encode(indexd.ListRecords{Records: []indexd.OutputInfo{}})
				}
				return
			}

		case r.Method == http.MethodPost && path == "/index/index":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"did":"did-1"}`))
			return
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/ga4gh/drs/v1/objects"):
			if path == "/ga4gh/drs/v1/objects" {
				m.mu.Lock()
				page := m.listObjectsPages
				m.listObjectsPages++
				m.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				if page == 0 {
					_ = encoder.NewStreamEncoder(w).Encode(drs.DRSPage{DRSObjects: []drs.DRSObject{sampleDataClientDRSObject()}})
				} else {
					_ = encoder.NewStreamEncoder(w).Encode(drs.DRSPage{DRSObjects: []drs.DRSObject{}})
				}
				return
			}
			obj := sampleOutputObject()
			w.WriteHeader(http.StatusOK)
			_ = encoder.NewStreamEncoder(w).Encode(obj)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/index/index/"):
			record := sampleDataClientOutputInfo()
			record.Rev = "rev-1"
			w.WriteHeader(http.StatusOK)
			_ = encoder.NewStreamEncoder(w).Encode(record)
			return
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/index/index/"):
			body, _ := ioReadAll(r)
			payload := indexd.UpdateInputInfo{}
			_ = sonic.ConfigFastest.Unmarshal(body, &payload)
			m.mu.Lock()
			m.lastUpdatePayload = payload
			m.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		case r.Method == http.MethodGet && strings.Contains(path, "/access/"):
			// Handle GetDownloadURL: /ga4gh/drs/v1/objects/{id}/access/{access_id}
			// Respond with AccessURL
			resp := map[string]string{
				"url": "https://bucket.s3.amazonaws.com/key",
			}
			w.WriteHeader(http.StatusOK)
			_ = encoder.NewStreamEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

func sampleDataClientOutputInfo() indexd.OutputInfo {
	return indexd.OutputInfo{
		Did:      "did-1",
		FileName: "file.txt",
		URLs:     []string{"s3://bucket/key"},
		Authz:    []string{"/programs/test/projects/proj"},
		Hashes:   hash.HashInfo{SHA256: "sha-256"},
		Size:     123,
	}
}

func sampleDataClientDRSObject() drs.DRSObject {
	return drs.DRSObject{
		Id:   "did-1",
		Name: "file.txt",
		Size: 123,
		Checksums: hash.HashInfo{
			SHA256: "sha-256",
		},
		AccessMethods: []drs.AccessMethod{
			{
				Type:           "s3",
				AccessURL:      drs.AccessURL{URL: "s3://bucket/key"},
				Authorizations: &drs.Authorizations{Value: "/programs/test/projects/proj"},
			},
		},
	}
}

func sampleOutputObject() indexd.OutputObject {
	return indexd.OutputObject{
		Id:   "did-1",
		Name: "file.txt",
		Size: 123,
		Checksums: []hash.Checksum{
			{Checksum: "sha-256", Type: hash.ChecksumTypeSHA256},
		},
	}
}

func newTestClient(server *httptest.Server) *GitDrsIdxdClient {
	base, _ := url.Parse(server.URL)
	cred := &conf.Credential{APIEndpoint: server.URL, Profile: "test"}
	// Create a dummy logger
	logger, _ := logs.New("test")
	// Convert logging because data-client now expects slog
	g3 := g3client.NewGen3InterfaceFromCredential(cred, logger, g3client.WithClients(g3client.IndexdClient, g3client.FenceClient, g3client.SowerClient))

	// Since we migrated GitDrsIdxdClient to accept slog.Logger but use TEE logger internally via bridge,
	// here we can just create a noop slog logger.
	sLog := logs.NewSlogNoOpLogger()

	config := &Config{
		ProjectId:          "test-proj",
		BucketName:         "bucket",
		Upsert:             false,
		MultiPartThreshold: 500 * common.MB,
	}

	return &GitDrsIdxdClient{
		Base:   base,
		Logger: sLog,
		G3:     g3,
		Config: config,
	}
}

func TestIndexdClient_ListAndQuery(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	records, err := client.GetObjectByHash(context.Background(), &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: "sha-256"})
	if err != nil {
		t.Fatalf("GetObjectByHash error: %v", err)
	}
	if len(records) != 1 || records[0].Id != "did-1" {
		t.Fatalf("unexpected records: %+v", records)
	}

	objChan, err := client.ListObjectsByProject(context.Background(), "test-proj")
	if err != nil {
		t.Fatalf("ListObjectsByProject error: %v", err)
	}
	var found bool
	for res := range objChan {
		if res.Error != nil {
			t.Fatalf("ListObjectsByProject result error: %v", res.Error)
		}
		if res.Object != nil && res.Object.Id == "did-1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected object from ListObjectsByProject")
	}

	listChan, err := client.ListObjects(context.Background())
	if err != nil {
		t.Fatalf("ListObjects error: %v", err)
	}
	var listCount int
	for res := range listChan {
		if res.Error != nil {
			t.Fatalf("ListObjects result error: %v", res.Error)
		}
		if res.Object != nil {
			listCount++
		}
	}
	if listCount != 1 {
		t.Fatalf("expected 1 object from ListObjects, got %d", listCount)
	}
}

func TestIndexdClient_RegisterAndUpdate(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	drsObj := &drs.DRSObject{
		Id:        "did-1",
		Name:      "file.txt",
		Size:      123,
		Checksums: hash.HashInfo{SHA256: "sha-256"},
		AccessMethods: []drs.AccessMethod{
			{
				Type:           "s3",
				AccessURL:      drs.AccessURL{URL: "s3://bucket/key"},
				Authorizations: &drs.Authorizations{Value: "/programs/test/projects/proj"},
			},
		},
	}

	obj, err := client.RegisterRecord(context.Background(), drsObj)
	if err != nil {
		t.Fatalf("RegisterRecord error: %v", err)
	}
	if obj.Id != "did-1" {
		t.Fatalf("unexpected DRS object: %+v", obj)
	}

	update := &drs.DRSObject{
		Name:        "file-updated.txt",
		Version:     "v2",
		Description: "updated",
		AccessMethods: []drs.AccessMethod{
			{
				Type:           "s3",
				AccessURL:      drs.AccessURL{URL: "s3://bucket/other"},
				Authorizations: &drs.Authorizations{Value: "/programs/test/projects/proj"},
			},
		},
	}

	_, err = client.UpdateRecord(context.Background(), update, "did-1")
	if err != nil {
		t.Fatalf("UpdateRecord error: %v", err)
	}

	mock.mu.Lock()
	payload := mock.lastUpdatePayload
	mock.mu.Unlock()

	if len(payload.URLs) != 2 {
		t.Fatalf("expected URLs to include appended entries, got %+v", payload.URLs)
	}
}

func TestIndexdClient_GetObject(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	record, err := client.GetObject(context.Background(), "did-1")
	if err != nil {
		t.Fatalf("GetObject error: %v", err)
	}
	if record.Id != "did-1" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestIndexdClient_GetProjectSample(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	sample, err := client.GetProjectSample(context.Background(), "test-proj", 1)
	if err != nil {
		t.Fatalf("GetProjectSample error: %v", err)
	}
	if len(sample) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(sample))
	}
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	return repoDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
}
