package local

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/download"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/localclient"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/data-client/transfer"
	"github.com/calypr/data-client/upload"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
	"github.com/calypr/git-drs/lfs"
)

type LocalRemote struct {
	BaseURL       string
	ProjectID     string
	Bucket        string
	Organization  string
	StoragePrefix string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}
func (l LocalRemote) GetOrganization() string { return l.Organization }
func (l LocalRemote) GetEndpoint() string     { return l.BaseURL }
func (l LocalRemote) GetBucketName() string   { return l.Bucket }
func (l LocalRemote) GetStoragePrefix() string {
	return l.StoragePrefix
}
func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	return NewLocalClient(l, logger), nil
}

type LocalConfig struct {
	MultiPartThreshold int64
	UploadConcurrency  int
}

type LocalClient struct {
	Remote      LocalRemote
	Logger      *slog.Logger
	DataLogger  *logs.Gen3Logger
	meta        metadataStore
	uploads     uploadService
	downloads   downloadService
	Config      *LocalConfig
	LocalFacade localclient.LocalInterface
}

var errNoLocalRecordsForOID = errors.New("no records found for OID")

type metadataStore interface {
	GetObject(ctx context.Context, id string) (*drs.DRSObject, error)
	ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error)
	ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error)
	GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error)
	BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error)
	DeleteRecordsByProject(ctx context.Context, projectId string) error
	DeleteRecord(ctx context.Context, did string) error
	GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error)
	RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error)
	RegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error)
	UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error)
}

type uploadService interface {
	Upload(ctx context.Context, req common.FileUploadRequestObject) error
}

type downloadService interface {
	ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error)
	DownloadToPath(ctx context.Context, guid string, dstPath string, opts download.DownloadOptions) error
}

func NewLocalClient(remote LocalRemote, logger *slog.Logger) *LocalClient {
	cred := &conf.Credential{APIEndpoint: remote.BaseURL}
	dataLogger := logs.NewGen3Logger(logger, "", "")
	facade := localclient.NewLocalInterfaceFromCredential(cred, dataLogger)

	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	multiPartThreshold := int64(multiPartThresholdInt) * common.MB
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	server := facade.DRSClient()
	md := server.
		WithProject(remote.GetProjectId()).
		WithOrganization(remote.GetOrganization()).
		WithBucket(remote.GetBucketName())

	uploader := &serverUploadService{backend: server}
	downloader := &serverDownloadService{
		client:  md,
		backend: server,
		logger:  logger,
	}

	return &LocalClient{
		Remote:      remote,
		Logger:      logger,
		DataLogger:  dataLogger,
		meta:        md,
		uploads:     uploader,
		downloads:   downloader,
		Config:      &LocalConfig{MultiPartThreshold: multiPartThreshold, UploadConcurrency: uploadConcurrency},
		LocalFacade: facade,
	}
}

func (c *LocalClient) GetProjectId() string                     { return c.Remote.GetProjectId() }
func (c *LocalClient) GetBucketName() string                    { return c.Remote.GetBucketName() }
func (c *LocalClient) GetOrganization() string                  { return c.Remote.Organization }
func (c *LocalClient) GetGen3Interface() g3client.Gen3Interface { return nil }

func (c *LocalClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return c.meta.GetObject(ctx, id)
}
func (c *LocalClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return c.meta.ListObjects(ctx)
}
func (c *LocalClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	return c.meta.ListObjectsByProject(ctx, project)
}
func (c *LocalClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	u, err := c.downloads.ResolveDownloadURL(ctx, oid, "")
	if err != nil {
		return nil, err
	}
	return &drs.AccessURL{Url: u}, nil
}
func (c *LocalClient) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	return c.meta.GetObjectByHash(ctx, checksum)
}
func (c *LocalClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return c.meta.BatchGetObjectsByHash(ctx, hashes)
}
func (c *LocalClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	return c.meta.DeleteRecordsByProject(ctx, project)
}
func (c *LocalClient) DeleteRecordByOID(ctx context.Context, oid string) error {
	records, err := c.meta.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		return fmt.Errorf("error resolving DRS object for OID %s: %w", oid, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%w %s", errNoLocalRecordsForOID, oid)
	}

	var did string
	if c.GetOrganization() != "" && c.GetProjectId() != "" {
		match, matchErr := drsmap.FindMatchingRecord(records, c.GetOrganization(), c.GetProjectId())
		if matchErr != nil {
			return fmt.Errorf("error finding matching record for project %s: %w", c.GetProjectId(), matchErr)
		}
		if match == nil {
			return fmt.Errorf("no matching record found for project %s", c.GetProjectId())
		}
		did = match.Id
	} else {
		did = records[0].Id
	}

	return c.DeleteRecordByDID(ctx, did)
}
func (c *LocalClient) DeleteRecordByDID(ctx context.Context, did string) error {
	return c.meta.DeleteRecord(ctx, did)
}
func (c *LocalClient) GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error) {
	return c.meta.GetProjectSample(ctx, projectId, limit)
}
func (c *LocalClient) RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error) {
	return c.meta.RegisterRecord(ctx, record)
}
func (c *LocalClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return c.meta.RegisterRecords(ctx, records)
}
func (c *LocalClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return c.meta.UpdateRecord(ctx, updateInfo, did)
}

func (c *LocalClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	builder := drs.NewObjectBuilder(c.Remote.GetBucketName(), c.Remote.GetProjectId())
	builder.Organization = c.Remote.Organization
	builder.StoragePrefix = c.Remote.StoragePrefix
	builder.PathStyle = "CAS"
	return builder.Build(fileName, checksum, size, drsId)
}

func (c *LocalClient) RegisterFile(ctx context.Context, oid string, filePath string) (*drs.DRSObject, error) {
	oid = drs.NormalizeOid(oid)
	obj, err := drsmap.DrsInfoFromOid(oid)
	if err != nil || obj == nil {
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record: %v", statErr)
		}
		drsID := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
		obj, err = c.BuildDrsObj(filepath.Base(filePath), oid, stat.Size(), drsID)
		if err != nil {
			return nil, err
		}
	}

	registered, err := c.RegisterRecord(ctx, obj)
	if err != nil {
		return nil, err
	}
	obj = registered

	if c.Remote.GetBucketName() == "" {
		return obj, nil
	}

	uploadReq := common.FileUploadRequestObject{
		SourcePath:   filePath,
		ObjectKey:    filepath.Base(filePath),
		GUID:         obj.Id,
		FileMetadata: common.FileMetadata{},
		Bucket:       c.Remote.GetBucketName(),
	}
	if err := c.uploads.Upload(ctx, uploadReq); err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *LocalClient) DownloadFile(ctx context.Context, guid string, destPath string) error {
	opts := download.DownloadOptions{MultipartThreshold: int64(5 * common.GB)}
	if c.Config != nil && c.Config.MultiPartThreshold > 0 {
		opts.MultipartThreshold = c.Config.MultiPartThreshold
	}
	return c.downloads.DownloadToPath(ctx, guid, destPath, opts)
}

func (c *LocalClient) BatchSyncForPush(ctx context.Context, files map[string]lfs.LfsFileInfo) error {
	for _, f := range files {
		if _, err := c.RegisterFile(ctx, f.Oid, f.Name); err != nil {
			return fmt.Errorf("upload failed for %s (%s): %w", f.Name, f.Oid, err)
		}
	}
	return nil
}

type serverUploadService struct {
	backend transfer.Uploader
}

func (s *serverUploadService) Upload(ctx context.Context, req common.FileUploadRequestObject) error {
	return upload.Upload(ctx, s.backend, req, false)
}

type serverDownloadService struct {
	client  drs.Client
	backend transfer.Downloader
	logger  *slog.Logger
}

func (s *serverDownloadService) ResolveDownloadURL(ctx context.Context, guid string, accessID string) (string, error) {
	return s.backend.ResolveDownloadURL(ctx, guid, accessID)
}

func (s *serverDownloadService) DownloadToPath(ctx context.Context, guid string, dstPath string, opts download.DownloadOptions) error {
	return download.DownloadToPathWithOptions(ctx, s.client, s.backend, s.logger, guid, dstPath, "", opts)
}
