package transfer

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drs/hash"
	"github.com/calypr/git-drs/lfs"
	"github.com/calypr/git-drs/s3_utils"
)

type mockClient struct {
	downloadURL string
	registerObj *drs.DRSObject
}

func (m mockClient) GetProjectId() string                           { return "project" }
func (m mockClient) GetObject(id string) (*drs.DRSObject, error)    { return nil, nil }
func (m mockClient) ListObjects() (chan drs.DRSObjectResult, error) { return nil, nil }
func (m mockClient) ListObjectsByProject(project string) (chan drs.DRSObjectResult, error) {
	return nil, nil
}
func (m mockClient) GetDownloadURL(oid string) (*drs.AccessURL, error) {
	return &drs.AccessURL{URL: m.downloadURL}, nil
}
func (m mockClient) GetObjectByHash(sum *hash.Checksum) ([]drs.DRSObject, error) { return nil, nil }
func (m mockClient) DeleteRecordsByProject(project string) error                 { return nil }
func (m mockClient) DeleteRecord(oid string) error                               { return nil }
func (m mockClient) RegisterRecord(indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	return nil, nil
}
func (m mockClient) RegisterFile(oid string) (*drs.DRSObject, error) { return m.registerObj, nil }
func (m mockClient) UpdateRecord(updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return nil, nil
}
func (m mockClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return nil, nil
}
func (m mockClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...s3_utils.AddURLOption) (s3_utils.S3Meta, error) {
	return s3_utils.S3Meta{}, nil
}

func TestDownloadWorker(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()

	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	msg := lfs.DownloadMessage{Event: "download", Oid: oid}
	payload, err := sonic.ConfigFastest.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jobs := make(chan TransferJob, 1)
	results := make(chan TransferResult, 1)
	go downloadWorker(1, jobs, results)

	jobs <- TransferJob{data: payload, drsClient: mockClient{downloadURL: server.URL}}
	close(jobs)

	res := <-results
	complete, ok := res.data.(lfs.CompleteMessage)
	if !ok {
		t.Fatalf("expected CompleteMessage, got %T", res.data)
	}
	if complete.Oid != oid {
		t.Fatalf("unexpected oid: %s", complete.Oid)
	}
	if filepath.Base(complete.Path) != oid {
		t.Fatalf("unexpected path: %s", complete.Path)
	}
}

func TestUploadWorker(t *testing.T) {
	oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	msg := lfs.UploadMessage{Event: "upload", Oid: oid}
	payload, err := sonic.ConfigFastest.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jobs := make(chan TransferJob, 1)
	results := make(chan TransferResult, 1)
	go uploadWorker(1, jobs, results)

	jobs <- TransferJob{data: payload, drsClient: mockClient{registerObj: &drs.DRSObject{Name: "file.txt"}}}
	close(jobs)

	res := <-results
	complete, ok := res.data.(lfs.CompleteMessage)
	if !ok {
		t.Fatalf("expected CompleteMessage, got %T", res.data)
	}
	if complete.Path != "file.txt" {
		t.Fatalf("unexpected path: %s", complete.Path)
	}
}

func TestDownloadWorker_InvalidJSON(t *testing.T) {
	jobs := make(chan TransferJob, 1)
	results := make(chan TransferResult, 1)
	go downloadWorker(1, jobs, results)

	jobs <- TransferJob{data: []byte("bad"), drsClient: mockClient{}}
	close(jobs)

	res := <-results
	if res.isError == false {
		t.Fatalf("expected error result")
	}
}

func TestUploadWorker_InvalidJSON(t *testing.T) {
	jobs := make(chan TransferJob, 1)
	results := make(chan TransferResult, 1)
	go uploadWorker(1, jobs, results)

	jobs <- TransferJob{data: []byte("bad"), drsClient: mockClient{}}
	close(jobs)

	res := <-results
	if res.isError == false {
		t.Fatalf("expected error result")
	}
}
