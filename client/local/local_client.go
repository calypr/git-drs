package local

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/calypr/data-client/backend"
	drs_backend "github.com/calypr/data-client/backend/drs"
	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/download"
	drs "github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/data-client/request"
	"github.com/calypr/data-client/s3utils"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/cloud"
	"github.com/calypr/git-drs/drsmap"
)

type LocalRemote struct {
	BaseURL      string
	ProjectID    string
	Bucket       string
	Organization string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}

func (l LocalRemote) GetOrganization() string {
	return l.Organization
}

func (l LocalRemote) GetEndpoint() string {
	return l.BaseURL
}

func (l LocalRemote) GetBucketName() string {
	return l.Bucket
}

func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	return NewLocalClient(l, logger), nil
}

type LocalClient struct {
	Remote  LocalRemote
	Logger  *slog.Logger
	Backend backend.Backend
}

func NewLocalClient(remote LocalRemote, logger *slog.Logger) *LocalClient {
	// Initialize RequestInterface for DrsBackend
	cred := &conf.Credential{
		APIEndpoint: remote.BaseURL,
	}
	gen3Logger := logs.NewGen3Logger(logger, "", "")
	cfg := conf.NewConfigure(logger)
	req := request.NewRequestInterface(gen3Logger, cred, cfg)

	// Initialize the DRS backend
	bk := drs_backend.NewDrsBackend(remote.BaseURL, logger, req)

	return &LocalClient{
		Remote:  remote,
		Logger:  logger,
		Backend: bk,
	}
}

// Helpers

// Helpers removed as they are replaced by data-client functionality

// Implement DRSClient interface

func (c *LocalClient) GetProjectId() string {
	return c.Remote.GetProjectId()
}

func (c *LocalClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return c.Backend.GetFileDetails(ctx, id)
}

func (c *LocalClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return nil, fmt.Errorf("ListObjects not implemented for LocalClient")
}

func (c *LocalClient) ListObjectsByProject(ctx context.Context, project string) (chan drs.DRSObjectResult, error) {
	return nil, fmt.Errorf("ListObjectsByProject not implemented for LocalClient")
}

func (c *LocalClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	// 1. Get Object to find access method
	obj, err := c.GetObject(ctx, oid)
	if err != nil {
		return nil, fmt.Errorf("failed to get object %s: %w", oid, err)
	}

	if len(obj.AccessMethods) == 0 {
		return nil, fmt.Errorf("no access methods found for object %s", oid)
	}

	// 2. Naively pick specific access method if we knew it, or just first one?
	// git-drs/client/indexd/client.go picks first one or tries to match accessType.
	// We'll pick the first one that has an access_id (indicating we need to call /access endpoint)
	// or matches a type we like (e.g. 's3', 'gs', 'http', 'https').
	// If AccessURL.URL is already present in AccessMethod (inline), use it.

	var accessID string

	// Prefer file://, then HTTP/HTTPS, then S3
	for _, am := range obj.AccessMethods {
		if strings.HasPrefix(am.AccessURL.URL, "file://") {
			return &am.AccessURL, nil
		}
	}

	for _, am := range obj.AccessMethods {
		if am.AccessURL.URL != "" {
			// Direct URL available
			return &am.AccessURL, nil
		}
		// If we have an AccessID, we can resolve it
		if am.AccessID != "" {
			accessID = am.AccessID
			break
		}
	}

	if accessID == "" {
		// Fallback to first if defined
		if len(obj.AccessMethods) > 0 && obj.AccessMethods[0].AccessID != "" {
			accessID = obj.AccessMethods[0].AccessID
		} else {
			return nil, fmt.Errorf("no suitable access method found for object %s", oid)
		}
	}

	// 3. Call /access endpoint using Backend
	url, err := c.Backend.GetDownloadURL(ctx, oid, accessID)
	if err != nil {
		return nil, err
	}
	return &drs.AccessURL{URL: url}, nil
}

func (c *LocalClient) GetObjectByHash(ctx context.Context, checksum *hash.Checksum) ([]drs.DRSObject, error) {
	res, err := c.Backend.GetObjectByHash(ctx, string(checksum.Type), checksum.Checksum)
	if err != nil {
		return nil, err
	}

	filtered := make([]drs.DRSObject, 0)
	for _, o := range res {
		matched, err := drsmap.FindMatchingRecord([]drs.DRSObject{o}, c.Remote.Organization, c.Remote.GetProjectId())
		if err == nil && matched != nil {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

func (c *LocalClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return c.Backend.BatchGetObjectsByHash(ctx, hashes)
}

func (c *LocalClient) DeleteRecordsByProject(ctx context.Context, project string) error {
	// Not supported by Backend interface yet, keeping as no-op or we can implement in backend if needed
	return fmt.Errorf("Not Implemented")
}

func (c *LocalClient) DeleteRecord(ctx context.Context, oid string) error {
	return fmt.Errorf("Not Implemented")
}

// RegisterRecord registers a DRS object with the server
func (c *LocalClient) RegisterRecord(ctx context.Context, indexdObject *drs.DRSObject) (*drs.DRSObject, error) {
	return c.Backend.Register(ctx, indexdObject)
}

func (c *LocalClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return c.Backend.BatchRegister(ctx, records)
}

func (c *LocalClient) RegisterFile(ctx context.Context, oid string, filePath string) (*drs.DRSObject, error) {
	// 1. Get info from local prepush result or file stat
	drsObject, err := drsmap.DrsInfoFromOid(oid)
	if err != nil {
		stat, statErr := os.Stat(filePath)
		if statErr != nil {
			return nil, fmt.Errorf("error reading local record: %v", statErr)
		}
		drsId := drsmap.DrsUUID(c.Remote.GetProjectId(), oid)
		drsObject, err = c.BuildDrsObj(filepath.Base(filePath), oid, stat.Size(), drsId)
		if err != nil {
			return nil, err
		}
	}

	// 2. Register Record
	c.Logger.InfoContext(ctx, fmt.Sprintf("registering record for oid %s in DRS server (did: %s)", oid, drsObject.Id))
	registeredObj, err := c.RegisterRecord(ctx, drsObject)
	if err != nil {
		return nil, fmt.Errorf("error registering record: %v", err)
	}
	drsObject = registeredObj

	// 3. Perform S3 Upload if bucket is configured using Backend logic for URL
	if c.Remote.GetBucketName() != "" {
		// Use CAS key (HASH) for upload to match Record URL
		uploadKey := drsObject.Checksums.SHA256
		uploadURL, err := c.Backend.GetUploadURL(ctx, drsObject.Id, uploadKey, common.FileMetadata{}, c.Remote.GetBucketName())
		if err != nil {
			return nil, fmt.Errorf("failed to get signed upload URL: %w", err)
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		defer file.Close()

		c.Logger.DebugContext(ctx, "performing S3 upload", "url", uploadURL)
		if err := c.Backend.Upload(ctx, uploadURL, file, drsObject.Size); err != nil {
			return nil, err
		}
	}

	return drsObject, nil
}

func (c *LocalClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	// Backend interface doesn't support UpdateRecord yet.
	return nil, fmt.Errorf("UpdateRecord not implemented for LocalClient")
}

func (c *LocalClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	builder := drs.NewObjectBuilder(c.Remote.GetBucketName(), c.Remote.GetProjectId())
	builder.Organization = c.Remote.Organization
	builder.PathStyle = "CAS"
	return builder.Build(fileName, checksum, size, drsId)
}

func (c *LocalClient) AddURL(s3URL, sha256, awsAccessKey, awsSecretKey, regionFlag, endpointFlag string, opts ...cloud.AddURLOption) (s3utils.S3Meta, error) {
	return s3utils.S3Meta{}, fmt.Errorf("AddURL not implemented for LocalClient")
}

func (c *LocalClient) GetBucketName() string {
	return c.Remote.GetBucketName()
}

func (c *LocalClient) GetOrganization() string {
	return c.Remote.Organization
}

func (c *LocalClient) GetGen3Interface() g3client.Gen3Interface {
	return nil
}

func (c *LocalClient) DownloadFile(ctx context.Context, guid string, destPath string) error {
	return download.DownloadToPath(ctx, c.Backend, c.Logger, guid, destPath, "")
}
