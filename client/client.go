package client

import (
	"context"

	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/git-drs/lfs"
)

// DRSClient is the shared git-drs client surface used by commands and drsmap.
type DRSClient interface {
	GetProjectId() string
	GetObject(ctx context.Context, id string) (*drs.DRSObject, error)
	ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error)
	ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error)
	GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error)
	GetObjectByHash(ctx context.Context, sum *hash.Checksum) ([]drs.DRSObject, error)
	BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error)
	DeleteRecordsByProject(ctx context.Context, projectId string) error
	DeleteRecordByOID(ctx context.Context, oid string) error
	DeleteRecordByDID(ctx context.Context, did string) error
	GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error)
	RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error)
	BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error)
	UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)
	BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error)
	GetGen3Interface() g3client.Gen3Interface
	GetBucketName() string
	GetOrganization() string
	RegisterFile(ctx context.Context, oid string, path string) (*drs.DRSObject, error)
	DownloadFile(ctx context.Context, oid string, destPath string) error
	BatchSyncForPush(ctx context.Context, files map[string]lfs.LfsFileInfo) error
}
