package drs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/download"
	"github.com/calypr/data-client/drs"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
)

// Config holds configuration parameters for the GitDrsIdxdClient.
type Config struct {
	ProjectId          string
	BucketName         string
	Organization       string
	StoragePrefix      string
	Upsert             bool
	MultiPartThreshold int64
	UploadConcurrency  int
}

type GitDrsClient struct {
	Base   *url.URL
	Logger *slog.Logger
	G3     g3client.Gen3Interface
	Config *Config
}

var errNoRecordsForOID = errors.New("no records found for OID")

func NewGitDrsClient(profileConfig conf.Credential, remote Gen3Remote, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	baseUrl, err := url.Parse(profileConfig.APIEndpoint)
	if err != nil {
		return nil, err
	}

	projectId := remote.GetProjectId()
	if projectId == "" {
		return nil, fmt.Errorf("no gen3 project specified")
	}

	scope, err := gitrepo.ResolveBucketScope(
		remote.GetOrganization(),
		projectId,
		remote.GetBucketName(),
		remote.GetStoragePrefix(),
	)
	if err != nil {
		return nil, err
	}
	bucketName := scope.Bucket

	// Initialize data-client Gen3Interface with slog-adapted logger if needed.
	enableDataClientLogs := gitrepo.GetGitConfigBool("drs.enable-data-client-logs", false)

	logOpts := []logs.Option{
		logs.WithBaseLogger(logger),
		logs.WithNoConsole(),
	}

	if enableDataClientLogs {
		logOpts = append(logOpts, logs.WithMessageFile())
	} else {
		logOpts = append(logOpts, logs.WithNoMessageFile())
	}

	dLogger, closer := logs.New(profileConfig.Profile, logOpts...)
	_ = closer

	var g3 g3client.Gen3Interface
	if len(opts) == 0 {
		g3 = g3client.NewGen3InterfaceFromCredential(&profileConfig, dLogger)
	} else {
		g3 = g3client.NewGen3InterfaceFromCredential(&profileConfig, dLogger, opts...)
	}

	// Configure the DRS client with git-specific context
	g3.DRSClient().
		WithProject(projectId).
		WithOrganization(remote.GetOrganization()).
		WithBucket(bucketName)

	upsert := gitrepo.GetGitConfigBool("drs.upsert", false)
	multiPartThresholdInt := gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)
	var multiPartThreshold int64 = multiPartThresholdInt * common.MB
	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	config := &Config{
		ProjectId:          projectId,
		BucketName:         bucketName,
		Organization:       remote.GetOrganization(),
		StoragePrefix:      scope.Prefix,
		Upsert:             upsert,
		MultiPartThreshold: multiPartThreshold,
		UploadConcurrency:  uploadConcurrency,
	}

	return &GitDrsClient{
		Base:   baseUrl,
		Logger: logger,
		G3:     g3,
		Config: config,
	}, nil
}

func (cl *GitDrsClient) GetProjectId() string {
	return cl.Config.ProjectId
}

func (cl *GitDrsClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return cl.G3.DRSClient().GetObject(ctx, id)
}

func (cl *GitDrsClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return cl.G3.DRSClient().ListObjects(ctx)
}

func (cl *GitDrsClient) ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error) {
	return cl.G3.DRSClient().ListObjectsByProject(ctx, projectId)
}

func (cl *GitDrsClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	cl.Logger.Debug(fmt.Sprintf("Try to get download url for file OID %s", oid))

	records, err := cl.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		cl.Logger.Debug(fmt.Sprintf("error getting DRS object for OID %s: %s", oid, err))
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}
	return cl.getDownloadURLFromRecords(ctx, oid, records)
}

func (cl *GitDrsClient) getDownloadURLFromRecords(ctx context.Context, oid string, records []drs.DRSObject) (*drs.AccessURL, error) {
	if len(records) == 0 {
		cl.Logger.Debug(fmt.Sprintf("no DRS object found for OID %s", oid))
		return nil, fmt.Errorf("no DRS object found for OID %s", oid)
	}

	matchingRecord, err := drsmap.FindMatchingRecord(records, cl.GetOrganization(), cl.Config.ProjectId)
	if err != nil {
		cl.Logger.Debug(fmt.Sprintf("error finding matching record for project %s: %s", cl.Config.ProjectId, err))
		return nil, fmt.Errorf("error finding matching record for project %s: %v", cl.Config.ProjectId, err)
	}
	if matchingRecord == nil {
		cl.Logger.Debug(fmt.Sprintf("no matching record found for project %s", cl.Config.ProjectId))
		return nil, fmt.Errorf("no matching record found for project %s", cl.Config.ProjectId)
	}

	cl.Logger.Debug(fmt.Sprintf("Matching record: %#v for oid %s", matchingRecord, oid))
	drsObj := matchingRecord

	if len(drsObj.AccessMethods) == 0 {
		cl.Logger.Debug(fmt.Sprintf("no access methods available for DRS object %s", drsObj.Id))
		return nil, fmt.Errorf("no access methods available for DRS object %s", drsObj.Id)
	}

	accessType := drsObj.AccessMethods[0].Type
	if accessType == "" {
		cl.Logger.Debug(fmt.Sprintf("no accessType found in access method for DRS object %v", drsObj.AccessMethods[0]))
		return nil, fmt.Errorf("no accessType found in access method for DRS object %v", drsObj.AccessMethods[0])
	}
	did := drsObj.Id

	return cl.G3.DRSClient().GetDownloadURL(ctx, did, accessType)
}

func (cl *GitDrsClient) GetObjectByHash(ctx context.Context, sum *hash.Checksum) ([]drs.DRSObject, error) {
	res, err := cl.G3.DRSClient().GetObjectByHash(ctx, sum)
	if err != nil {
		return nil, err
	}

	resourcePath, err := drs.ProjectToResource(cl.Config.Organization, cl.Config.ProjectId)
	if err != nil {
		return nil, err
	}

	filtered := make([]drs.DRSObject, 0)
	for _, o := range res {
		found := false
		for _, am := range o.AccessMethods {
			if am.Authorizations != nil {
				for _, issuer := range am.Authorizations.BearerAuthIssuers {
					if issuer == resourcePath {
						found = true
						break
					}
				}
			}
			if found {
				break
			}
		}
		if found {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

func (cl *GitDrsClient) BatchGetObjectsByHash(ctx context.Context, hashes []string) (map[string][]drs.DRSObject, error) {
	return cl.G3.DRSClient().BatchGetObjectsByHash(ctx, hashes)
}

func (cl *GitDrsClient) DeleteRecordsByProject(ctx context.Context, projectId string) error {
	return cl.G3.DRSClient().DeleteRecordsByProject(ctx, projectId)
}

func (cl *GitDrsClient) DeleteRecordByOID(ctx context.Context, oid string) error {
	records, err := cl.G3.DRSClient().GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		return fmt.Errorf("error resolving DRS object for OID %s: %w", oid, err)
	}
	if len(records) == 0 {
		return fmt.Errorf("%w %s", errNoRecordsForOID, oid)
	}
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		did := strings.TrimSpace(record.Id)
		if did == "" {
			continue
		}
		if _, exists := seen[did]; exists {
			continue
		}
		seen[did] = struct{}{}
		if err := cl.DeleteRecordByDID(ctx, did); err != nil {
			return fmt.Errorf("error deleting DID %s for OID %s: %w", did, oid, err)
		}
	}
	if len(seen) == 0 {
		return fmt.Errorf("no deleteable DIDs found for OID %s", oid)
	}
	return nil
}

func (cl *GitDrsClient) DeleteRecordByDID(ctx context.Context, did string) error {
	return cl.G3.DRSClient().DeleteRecord(ctx, did)
}

func (cl *GitDrsClient) GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error) {
	return cl.G3.DRSClient().GetProjectSample(ctx, projectId, limit)
}

func (c *GitDrsClient) RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error) {
	return c.G3.DRSClient().RegisterRecord(ctx, record)
}

func (c *GitDrsClient) BatchRegisterRecords(ctx context.Context, records []*drs.DRSObject) ([]*drs.DRSObject, error) {
	return c.G3.DRSClient().RegisterRecords(ctx, records)
}

func (c *GitDrsClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return c.G3.DRSClient().UpdateRecord(ctx, updateInfo, did)
}

func (c *GitDrsClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return drs.BuildDrsObjWithPrefix(fileName, checksum, size, drsId, c.Config.BucketName, c.Config.Organization, c.Config.ProjectId, c.Config.StoragePrefix)
}

func (cl *GitDrsClient) GetGen3Interface() g3client.Gen3Interface {
	return cl.G3
}

func (cl *GitDrsClient) GetBucketName() string {
	return cl.Config.BucketName
}

func (cl *GitDrsClient) GetOrganization() string {
	return cl.Config.Organization
}

func (cl *GitDrsClient) GetUpsert() bool {
	return cl.Config.Upsert
}

func (cl *GitDrsClient) DownloadFile(ctx context.Context, oid string, destPath string) error {
	opts := download.DownloadOptions{
		MultipartThreshold: int64(5 * common.MB),
	}
	if cl.Config != nil && cl.Config.MultiPartThreshold > 0 {
		opts.MultipartThreshold = cl.Config.MultiPartThreshold
	}
	return download.DownloadToPathWithOptions(ctx, cl.G3.DRSClient(), cl.G3.DRSClient(), cl.Logger, oid, destPath, "", opts)
}
