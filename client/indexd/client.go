package indexd

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/indexd/drs"
	"github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/gitrepo"
)

// Config holds configuration parameters for the GitDrsIdxdClient.
type Config struct {
	ProjectId          string
	BucketName         string
	Upsert             bool
	MultiPartThreshold int64
}

type GitDrsIdxdClient struct {
	Base   *url.URL
	Logger *slog.Logger
	G3     g3client.Gen3Interface
	Config *Config
}

func NewGitDrsIdxdClient(profileConfig conf.Credential, remote Gen3Remote, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	baseUrl, err := url.Parse(profileConfig.APIEndpoint)
	if err != nil {
		return nil, err
	}

	projectId := remote.GetProjectId()
	if projectId == "" {
		return nil, fmt.Errorf("no gen3 project specified")
	}

	bucketName := remote.GetBucketName()

	// Initialize data-client Gen3Interface with slog-adapted logger if needed,
	// or assume we use the one passed in if we update data-client to take slog.
	// For now we assume data-client/logs/TeeLogger is still used by data-client internals,
	// so we bridge it.
	// Initialize data-client Gen3Interface with slog-adapted logger.
	// We disable data-client's console output because drslog already handles stderr/file logging.
	// We also disable data-client's separate message file by default to aggregate logs in git-drs.log,
	// but allow re-enabling it via config.
	// but allow re-enabling it via config.
	enableDataClientLogs := gitrepo.GetGitConfigBool("lfs.customtransfer.drs.enable-data-client-logs", false)

	logOpts := []logs.Option{
		logs.WithBaseLogger(logger),
		logs.WithNoConsole(), // drslog already writes to stderr if configured
	}

	if enableDataClientLogs {
		logOpts = append(logOpts, logs.WithMessageFile())
	} else {
		logOpts = append(logOpts, logs.WithNoMessageFile())
	}

	dLogger, closer := logs.New(profileConfig.Profile, logOpts...)
	_ = closer

	// If no options provided, use defaults for GitDrsIdxdClient
	if len(opts) == 0 {
		opts = append(opts, g3client.WithClients(g3client.IndexdClient, g3client.FenceClient, g3client.SowerClient))
	}
	g3 := g3client.NewGen3InterfaceFromCredential(&profileConfig, dLogger, opts...)

	upsert := gitrepo.GetGitConfigBool("lfs.customtransfer.drs.upsert", false)
	multiPartThresholdInt := gitrepo.GetGitConfigInt("lfs.customtransfer.drs.multipart-threshold", 500)
	var multiPartThreshold int64 = multiPartThresholdInt * common.MB

	config := &Config{
		ProjectId:          projectId,
		BucketName:         bucketName,
		Upsert:             upsert,
		MultiPartThreshold: multiPartThreshold,
	}

	return &GitDrsIdxdClient{
		Base:   baseUrl,
		Logger: logger,
		G3:     g3,
		Config: config,
	}, nil
}

func (cl *GitDrsIdxdClient) GetProjectId() string {
	return cl.Config.ProjectId
}

func (cl *GitDrsIdxdClient) GetObject(ctx context.Context, id string) (*drs.DRSObject, error) {
	return cl.G3.Indexd().GetObject(ctx, id)
}

func (cl *GitDrsIdxdClient) ListObjects(ctx context.Context) (chan drs.DRSObjectResult, error) {
	return cl.G3.Indexd().ListObjects(ctx)
}

func (cl *GitDrsIdxdClient) ListObjectsByProject(ctx context.Context, projectId string) (chan drs.DRSObjectResult, error) {
	return cl.G3.Indexd().ListObjectsByProject(ctx, projectId)
}

func (cl *GitDrsIdxdClient) GetDownloadURL(ctx context.Context, oid string) (*drs.AccessURL, error) {
	cl.Logger.Debug(fmt.Sprintf("Try to get download url for file OID %s", oid))

	// get the DRS object using the OID
	records, err := cl.GetObjectByHash(ctx, &hash.Checksum{Type: hash.ChecksumTypeSHA256, Checksum: oid})
	if err != nil {
		cl.Logger.Debug(fmt.Sprintf("error getting DRS object for OID %s: %s", oid, err))
		return nil, fmt.Errorf("error getting DRS object for OID %s: %v", oid, err)
	}
	return cl.getDownloadURLFromRecords(ctx, oid, records)
}

func (cl *GitDrsIdxdClient) getDownloadURLFromRecords(ctx context.Context, oid string, records []drs.DRSObject) (*drs.AccessURL, error) {
	if len(records) == 0 {
		cl.Logger.Debug(fmt.Sprintf("no DRS object found for OID %s", oid))
		return nil, fmt.Errorf("no DRS object found for OID %s", oid)
	}

	// Find a record that matches the client's project ID
	matchingRecord, err := drsmap.FindMatchingRecord(records, cl.Config.ProjectId)
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

	// Check if access methods exist
	if len(drsObj.AccessMethods) == 0 {
		cl.Logger.Debug(fmt.Sprintf("no access methods available for DRS object %s", drsObj.Id))
		return nil, fmt.Errorf("no access methods available for DRS object %s", drsObj.Id)
	}

	// naively get access ID from splitting first path into :
	accessType := drsObj.AccessMethods[0].Type
	if accessType == "" {
		cl.Logger.Debug(fmt.Sprintf("no accessType found in access method for DRS object %v", drsObj.AccessMethods[0]))
		return nil, fmt.Errorf("no accessType found in access method for DRS object %v", drsObj.AccessMethods[0])
	}
	did := drsObj.Id

	accessUrl, err := cl.G3.Indexd().GetDownloadURL(ctx, did, accessType)
	if err != nil {
		return nil, err
	}

	return &drs.AccessURL{URL: accessUrl.URL, Headers: accessUrl.Headers}, nil
}

func (cl *GitDrsIdxdClient) GetObjectByHash(ctx context.Context, sum *hash.Checksum) ([]drs.DRSObject, error) {
	res, err := cl.G3.Indexd().GetObjectByHash(ctx, string(sum.Type), sum.Checksum)
	if err != nil {
		return nil, err
	}

	// Filter by project ID logic is git-drs specific business logic (ensure we only see our project's files)
	resourcePath, err := common.ProjectToResource(cl.Config.ProjectId)
	if err != nil {
		return nil, err
	}

	filtered := make([]drs.DRSObject, 0)
	for _, o := range res {
		found := false
		for _, am := range o.AccessMethods {
			if am.Authorizations != nil && am.Authorizations.Value == resourcePath {
				found = true
				break
			}
		}
		if found {
			filtered = append(filtered, o)
		}
	}
	return filtered, nil
}

func (cl *GitDrsIdxdClient) DeleteRecordsByProject(ctx context.Context, projectId string) error {
	return cl.G3.Indexd().DeleteRecordsByProject(ctx, projectId)
}

func (cl *GitDrsIdxdClient) DeleteRecord(ctx context.Context, oid string) error {
	return cl.G3.Indexd().DeleteRecordByHash(ctx, oid, cl.Config.ProjectId)
}

func (cl *GitDrsIdxdClient) GetProjectSample(ctx context.Context, projectId string, limit int) ([]drs.DRSObject, error) {
	return cl.G3.Indexd().GetProjectSample(ctx, projectId, limit)
}

func (c *GitDrsIdxdClient) RegisterRecord(ctx context.Context, record *drs.DRSObject) (*drs.DRSObject, error) {
	return c.G3.Indexd().RegisterRecord(ctx, record)
}

func (c *GitDrsIdxdClient) UpdateRecord(ctx context.Context, updateInfo *drs.DRSObject, did string) (*drs.DRSObject, error) {
	return c.G3.Indexd().UpdateRecord(ctx, updateInfo, did)
}

func (c *GitDrsIdxdClient) BuildDrsObj(fileName string, checksum string, size int64, drsId string) (*drs.DRSObject, error) {
	return drs.BuildDrsObj(fileName, checksum, size, drsId, c.Config.BucketName, c.Config.ProjectId)
}

func (cl *GitDrsIdxdClient) GetGen3Interface() g3client.Gen3Interface {
	return cl.G3
}

func (cl *GitDrsIdxdClient) GetBucketName() string {
	return cl.Config.BucketName
}

func (cl *GitDrsIdxdClient) GetUpsert() bool {
	return cl.Config.Upsert
}
