package drsmap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	s3utils "github.com/calypr/data-client/s3utils"
	"github.com/calypr/git-drs/cloud"
	localCommon "github.com/calypr/git-drs/common"
	"github.com/calypr/git-drs/lfs"
)

func setupTestRepo(t *testing.T) {
	t.Helper()

	tmpDir := t.TempDir()
	cmd := exec.Command("git", "init", tmpDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(cwd)
	})
}

func TestWriteAndReadDrsObject(t *testing.T) {
	setupTestRepo(t)
	oid := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	path, err := GetObjectPath(".git/drs/lfs/objects", oid)
	if err != nil {
		t.Fatalf("GetObjectPath error: %v", err)
	}

	obj := &drs.DRSObject{
		Id:        "did-1",
		Name:      "file.txt",
		Checksums: hash.HashInfo{SHA256: oid},
	}

	if err := WriteDrsObj(obj, oid, path); err != nil {
		t.Fatalf("WriteDrsObj error: %v", err)
	}

	read, err := DrsInfoFromOid(oid)
	if err != nil {
		t.Fatalf("DrsInfoFromOid error: %v", err)
	}
	if read.Id != "did-1" {
		t.Fatalf("unexpected object: %+v", read)
	}
}

func TestGetObjectPathValidation(t *testing.T) {
	if _, err := GetObjectPath(".git/drs/lfs/objects", "short"); err == nil {
		t.Fatalf("expected error for invalid oid")
	}
}

func TestDrsUUIDDeterministic(t *testing.T) {
	id1 := DrsUUID("project", "hash")
	id2 := DrsUUID("project", "hash")
	if id1 != id2 {
		t.Fatalf("expected deterministic UUIDs, got %s vs %s", id1, id2)
	}
}

func TestGetObjectPathLayout(t *testing.T) {
	oid := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	path, err := GetObjectPath("base", oid)
	if err != nil {
		t.Fatalf("GetObjectPath error: %v", err)
	}
	if filepath.Base(path) != oid {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestWriteDrsFile(t *testing.T) {
	setupTestRepo(t)

	builder := drs.NewObjectBuilder("bucket", "prog-project")
	file := lfs.LfsFileInfo{
		Name: "file.txt",
		Size: 12,
		Oid:  "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}

	drsObj, err := WriteDrsFile(builder, file, nil)
	if err != nil {
		t.Fatalf("WriteDrsFile error: %v", err)
	}
	if drsObj.Id == "" {
		t.Fatalf("expected drs object id")
	}

	read, err := DrsInfoFromOid(file.Oid)
	if err != nil {
		t.Fatalf("DrsInfoFromOid error: %v", err)
	}
	if read.Checksums.SHA256 != file.Oid {
		t.Fatalf("unexpected checksum: %+v", read.Checksums)
	}
}

// MockDRSClient implements client.DRSClient for testing
type MockDRSClient struct {
	Objects []drs.DRSObjectResult
	Project string
}

func (m *MockDRSClient) GetProjectId() string {
	return m.Project
}

func (m *MockDRSClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	ch := make(chan drs.DRSObjectResult, len(m.Objects))
	go func() {
		defer close(ch)
		for _, obj := range m.Objects {
			ch <- obj
		}
	}()
	return ch, nil
}

func (m *MockDRSClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) GetObjectByHash(ctx context.Context, hash *hash.Checksum) ([]drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	return nil
}

func (m *MockDRSClient) DeleteRecord(ctx context.Context, oid string) error {
	return nil
}

func (m *MockDRSClient) RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) RegisterFile(ctx context.Context, oid string, path string) (*drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return &drs.DRSObject{
		Id:   drsId,
		Name: fileName,
		Size: size,
		Checksums: hash.HashInfo{
			SHA256: checksum,
		},
	}, nil
}

func (m *MockDRSClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...cloud.AddURLOption) (s3utils.S3Meta, error) {
	return s3utils.S3Meta{}, fmt.Errorf("not implemented")
}

func (m *MockDRSClient) GetGen3Interface() g3client.Gen3Interface {
	return nil
}

func (m *MockDRSClient) GetBucketName() string {
	return "mock-bucket"
}

func (m *MockDRSClient) GetOrganization() string {
	return ""
}

func (m *MockDRSClient) DownloadFile(ctx context.Context, oid string, destPath string) error {
	return fmt.Errorf("not implemented")
}

func TestPullRemoteDrsObjects(t *testing.T) {
	setupTestRepo(t)
	// mockClient and setup
	sha := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	mockClient := &MockDRSClient{
		Project: "test-project",
		Objects: []drs.DRSObjectResult{
			{
				Object: &drs.DRSObject{
					Id: "obj1",
					Checksums: hash.HashInfo{
						SHA256: sha,
					},
					Name: "test-file",
				},
			},
		},
	}

	// Create required directory structure (mimicking setup that might be missing)
	os.MkdirAll(localCommon.DRS_OBJS_PATH, 0755)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	err := PullRemoteDrsObjects(mockClient, logger)
	if err != nil {
		t.Fatalf("PullRemoteDrsObjects failed: %v", err)
	}

	// Verify file exists using correct project path variable
	// PullRemoteDrsObjects uses projectdir.DRS_OBJS_PATH
	path, err := GetObjectPath(localCommon.DRS_OBJS_PATH, sha)
	if err != nil {
		t.Fatalf("GetObjectPath failed: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Expected DRS object file to be created at %s", path)
	}
}
