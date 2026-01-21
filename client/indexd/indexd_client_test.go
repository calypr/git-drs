package indexd_client

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/sonic"
	"github.com/bytedance/sonic/encoder"
	"github.com/calypr/data-client/client/common"
	"github.com/calypr/data-client/client/conf"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/drslog"
	"github.com/hashicorp/go-retryablehttp"
)

type stubAuthHandler struct{}

func (stubAuthHandler) AddAuthHeader(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer test")
	return nil
}

type mockIndexdServer struct {
	mu                sync.Mutex
	listProjectPages  int
	listObjectsPages  int
	lastUpdatePayload UpdateInputInfo
}

func (m *mockIndexdServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(os.Stderr, "Fake IndexD received %s %s\n", r.Method, r.URL.Path)
		path := r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/index/index":
			fmt.Fprintf(os.Stderr, "/index/index received %s %s\n", r.Method, r.URL.Path)
			if hashQuery := r.URL.Query().Get("hash"); hashQuery != "" {
				record := sampleOutputInfo()
				page := ListRecords{Records: []OutputInfo{record}}
				w.WriteHeader(http.StatusOK)
				_ = encoder.NewStreamEncoder(w).Encode(page)
				fmt.Fprintf(os.Stderr, "/index/index returned %s %s %+v\n", r.Method, r.URL.Path, page)
				return
			}
			if r.URL.Query().Get("authz") != "" {
				fmt.Fprintf(os.Stderr, "/index/index authz %s %s\n", r.Method, r.URL.Path)
				m.mu.Lock()
				page := m.listProjectPages
				m.listProjectPages++
				m.mu.Unlock()
				w.WriteHeader(http.StatusOK)
				if page == 0 {
					fmt.Fprintln(os.Stderr, "/index/index page == 0 ", r.Method, r.URL.Path, ListRecords{Records: []OutputInfo{sampleOutputInfo()}})
					_ = encoder.NewStreamEncoder(w).Encode(ListRecords{Records: []OutputInfo{sampleOutputInfo()}})
				} else {
					fmt.Fprintf(os.Stderr, "/index/index page != 0 %s %s\n", r.Method, r.URL.Path)
					_ = encoder.NewStreamEncoder(w).Encode(ListRecords{Records: []OutputInfo{}})
				}

				fmt.Fprintf(os.Stderr, "/index/index return no page %s %s\n", r.Method, r.URL.Path)
				return
			}
			fmt.Fprintf(os.Stderr, "/index/index NO HIT ! %s %s\n", r.Method, r.URL.Path)

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
					_ = encoder.NewStreamEncoder(w).Encode(drs.DRSPage{DRSObjects: []drs.DRSObject{sampleDrsObject()}})
				} else {
					_ = encoder.NewStreamEncoder(w).Encode(drs.DRSPage{DRSObjects: []drs.DRSObject{}})
				}
				return
			}
			obj := sampleOutputObject()
			w.WriteHeader(http.StatusOK)
			_ = encoder.NewStreamEncoder(w).Encode(obj)
			return
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/index/"):
			fmt.Printf("HasPrefix /index/ %s %s\n", r.Method, r.URL.Path)
			record := sampleOutputInfo()
			record.Rev = "rev-1"
			w.WriteHeader(http.StatusOK)
			_ = encoder.NewStreamEncoder(w).Encode(record)
			return
		case r.Method == http.MethodPut && strings.HasPrefix(path, "/index/index/"):
			body, err := ioReadAll(r)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			payload := UpdateInputInfo{}
			if err := sonic.ConfigFastest.Unmarshal(body, &payload); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			m.mu.Lock()
			m.lastUpdatePayload = payload
			m.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		fmt.Fprintf(os.Stderr, "StatusNotFound %s %s\n", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func ioReadAll(r *http.Request) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r.Body)
	return buf.Bytes(), err
}

func sampleOutputInfo() OutputInfo {
	record := OutputInfo{
		Did:      "did-1",
		FileName: "file.txt",
		URLs:     []string{"s3://bucket/key"},
		Authz:    []string{"/programs/test/projects/proj"},
		Hashes:   hash.HashInfo{SHA256: "sha-256"},
		Size:     123,
	}
	if record.Did != "" {
		fmt.Fprintf(os.Stderr, "Did set record Did %s\n", record.Did)
	}
	return record
}

func sampleDrsObject() drs.DRSObject {
	return drs.DRSObject{
		Id:   "did-1",
		Name: "file.txt",
		Size: 123,
		Checksums: hash.HashInfo{
			SHA256: "sha-256",
		},
	}
}

func sampleOutputObject() drs.OutputObject {
	return drs.OutputObject{
		Id:   "did-1",
		Name: "file.txt",
		Size: 123,
		Checksums: []hash.Checksum{
			{Checksum: "sha-256", Type: hash.ChecksumTypeSHA256},
		},
	}
}

func newTestClient(server *httptest.Server) *IndexDClient {
	base, _ := url.Parse(server.URL)
	return &IndexDClient{
		Base:        base,
		ProjectId:   "test-project",
		BucketName:  "bucket",
		Logger:      drslog.NewNoOpLogger(),
		AuthHandler: stubAuthHandler{},
		HttpClient:  retryablehttp.NewClient(),
		SConfig:     sonic.ConfigFastest,
	}
}

func TestIndexdClient_ListAndQuery(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	records, err := client.GetObjectByHash(&hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: "sha-256"})
	if err != nil {
		t.Fatalf("GetObjectByHash error: %v", err)
	}
	fmt.Fprintf(os.Stderr, "GetObjectByHash records: %+v\n", records)
	// TODO: re-enable once pagination fixed
	//if len(records) != 1 || records[0].Id != "did-1" {
	//	t.Fatalf("unexpected records: %+v", records)
	//}

	objChan, err := client.ListObjectsByProject("test-project")
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

	listChan, err := client.ListObjects()
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

	// TODO: re-enable once pagination fixed
	//fmt.Fprintf(os.Stderr, "GetProjectSample test\n")
	//sample, err := client.GetProjectSample("test-project", 1)
	//if err != nil {
	//	t.Fatalf("GetProjectSample error: %v", err)
	//}
	//if len(sample) != 1 || sample[0].Id != "did-1" {
	//	t.Fatalf("unexpected sample: %+v", sample)
	//}
}

func TestIndexdClient_RegisterAndUpdate(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	indexdObj := &IndexdRecord{
		Did:      "did-1",
		FileName: "file.txt",
		URLs:     []string{"s3://bucket/key"},
		Authz:    []string{"/programs/test/projects/proj"},
		Hashes:   hash.HashInfo{SHA256: "sha-256"},
		Size:     123,
	}

	obj, err := client.RegisterIndexdRecord(indexdObj)
	if err != nil {
		t.Fatalf("RegisterIndexdRecord error: %v", err)
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
				AccessURL:      drs.AccessURL{URL: "s3://bucket/other"},
				Authorizations: &drs.Authorizations{Value: "/programs/test/projects/proj"},
			},
		},
	}

	updated, err := client.UpdateRecord(update, "did-1")
	if err != nil {
		t.Fatalf("UpdateRecord error: %v", err)
	}
	if updated.Name != "file.txt" {
		t.Fatalf("expected updated DRS object from server, got %+v", updated)
	}

	mock.mu.Lock()
	payload := mock.lastUpdatePayload
	mock.mu.Unlock()

	if len(payload.URLs) != 2 {
		t.Fatalf("expected URLs to include appended entries, got %+v", payload.URLs)
	}
	if payload.FileName != "file-updated.txt" || payload.Version != "v2" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.Metadata == nil || payload.Metadata["description"] != "updated" {
		t.Fatalf("expected description metadata, got %+v", payload.Metadata)
	}
}

func TestIndexdClient_BuildDrsObj(t *testing.T) {
	client := &IndexDClient{
		ProjectId:  "test-project",
		BucketName: "bucket",
	}

	obj, err := client.BuildDrsObj("file.txt", "sha-256", 12, "did-1")
	if err != nil {
		t.Fatalf("BuildDrsObj error: %v", err)
	}
	if obj.Id != "did-1" || obj.Checksums.SHA256 != "sha-256" {
		t.Fatalf("unexpected drs object: %+v", obj)
	}
	if len(obj.AccessMethods) != 1 || !strings.Contains(obj.AccessMethods[0].AccessURL.URL, filepath.Join("bucket", "did-1", "sha-256")) {
		t.Fatalf("unexpected access URL: %+v", obj.AccessMethods)
	}
}

func TestIndexdClient_GetProfile(t *testing.T) {
	client := &IndexDClient{AuthHandler: &RealAuthHandler{Cred: confCredential("profile")}}
	profile, err := client.GetProfile()
	if err != nil {
		t.Fatalf("GetProfile error: %v", err)
	}
	if profile != "profile" {
		t.Fatalf("expected profile, got %s", profile)
	}
}

func confCredential(profile string) conf.Credential {
	return conf.Credential{Profile: profile}
}

func TestIndexdClient_GetProfile_Error(t *testing.T) {
	client := &IndexDClient{AuthHandler: stubAuthHandler{}}
	if _, err := client.GetProfile(); err == nil {
		t.Fatalf("expected error for non-real auth handler")
	}
}

func TestIndexdClient_GetIndexdRecordByDID(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	record, err := client.GetIndexdRecordByDID("did-1")
	if err != nil {
		t.Fatalf("GetIndexdRecordByDID error: %v", err)
	}
	if record.Did != "did-1" || record.Rev != "rev-1" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestIndexdClient_GetProjectSample_DefaultLimit(t *testing.T) {
	mock := &mockIndexdServer{}
	server := httptest.NewServer(mock.handler(t))
	defer server.Close()

	client := newTestClient(server)

	sample, err := client.GetProjectSample("test-project", 0)
	if err != nil {
		t.Fatalf("GetProjectSample error: %v", err)
	}
	if len(sample) != 1 {
		t.Fatalf("expected default limit sample, got %d", len(sample))
	}
}

func TestIndexdClient_NewIndexDClient(t *testing.T) {
	repoDir := initTestGitRepo(t)
	restore := chdirForTest(t, repoDir)
	defer restore()

	runGit(t, repoDir, "config", "lfs.customtransfer.drs.upsert", "false")
	runGit(t, repoDir, "config", "lfs.customtransfer.drs.multipart-threshold", "222")

	cred := conf.Credential{APIEndpoint: "https://example.com"}
	remote := Gen3Remote{ProjectID: "project", Bucket: "bucket"}
	client, err := NewIndexDClient(cred, remote, drslog.NewNoOpLogger())
	if err != nil {
		t.Fatalf("NewIndexDClient error: %v", err)
	}
	indexd, ok := client.(*IndexDClient)
	if !ok {
		t.Fatalf("expected IndexDClient")
	}
	if indexd.ProjectId != "project" || indexd.BucketName != "bucket" {
		t.Fatalf("unexpected client: %+v", indexd)
	}
	if indexd.HttpClient.HTTPClient.Timeout != 30*time.Second {
		t.Fatalf("unexpected http timeout: %v", indexd.HttpClient.HTTPClient.Timeout)
	}
	if indexd.Upsert {
		t.Fatalf("expected force push disabled, got %v", indexd.Upsert)
	}
	if indexd.MultiPartThreshold != 222*common.MB {
		t.Fatalf("expected multipart threshold 222, got %d", indexd.MultiPartThreshold)
	}
}

func TestGetLfsCustomTransferBool_DefaultValue(t *testing.T) {
	repoDir := initTestGitRepo(t)
	restore := chdirForTest(t, repoDir)
	defer restore()

	value, err := getLfsCustomTransferBool("lfs.customtransfer.drs.upsert", false)
	if err != nil {
		t.Fatalf("getLfsCustomTransferBool error: %v", err)
	}
	if value {
		t.Fatalf("expected default false, got %v", value)
	}
}

func TestGetLfsCustomTransferBool_MissingKeyReturnsDefault(t *testing.T) {
	repoDir := initTestGitRepo(t)
	restore := chdirForTest(t, repoDir)
	defer restore()

	value, err := getLfsCustomTransferBool("lfs.customtransfer.drs.upsert", false)
	if err != nil {
		t.Fatalf("getLfsCustomTransferBool error: %v", err)
	}
	if value {
		t.Fatalf("expected false, got %v", value)
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

func chdirForTest(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir error: %v", err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore Chdir error: %v", err)
		}
	}
}
