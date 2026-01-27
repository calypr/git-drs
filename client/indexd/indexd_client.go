package indexd_client

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"github.com/calypr/data-client/common"
	"github.com/calypr/data-client/conf"
	dataClient "github.com/calypr/data-client/g3client"
	drs "github.com/calypr/data-client/indexd/drs"
	hash "github.com/calypr/data-client/indexd/hash"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/drslog"
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
	G3     dataClient.Gen3Interface
	Config *Config
}

func NewGitDrsIdxdClient(profileConfig conf.Credential, remote Gen3Remote, logger *slog.Logger) (client.DRSClient, error) {
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
	dLogger, closer := logs.New(profileConfig.Profile, logs.WithBaseLogger(drslog.AsStdLogger(logger)))
	// Note: closer is ignored here
	_ = closer

	g3 := dataClient.NewGen3InterfaceFromCredential(&profileConfig, dLogger)

	upsert, err := getLfsCustomTransferBool("lfs.customtransfer.drs.upsert", false)
	if err != nil {
		return nil, err
	}
	multiPartThresholdInt, err := getLfsCustomTransferInt("lfs.customtransfer.drs.multipart-threshold", 500)
	if err != nil {
		return nil, err
	}
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

func (cl *GitDrsIdxdClient) GetDownloadURL(ctx context.Context, did string) (*drs.AccessURL, error) {
	// Delegate fully to data-client if possible?
	// data-client has GetDownloadURL but it might need accessType.
	// The wrapper logic here to get object first to find accessType is specific logic.
	// Ideally data-client's GetDownloadURL handles this or we keep this logic here.

	// First get the object to find access methods
	obj, err := cl.GetObject(ctx, did)
	if err != nil {
		return nil, err
	}
	if len(obj.AccessMethods) == 0 {
		return nil, fmt.Errorf("no access methods for %s", did)
	}

	accessType := obj.AccessMethods[0].Type
	res, err := cl.G3.Indexd().GetDownloadURL(ctx, did, accessType)
	if err != nil {
		return nil, err
	}
	return &drs.AccessURL{URL: res.URL, Headers: res.Headers}, nil
}

func (cl *GitDrsIdxdClient) GetObjectByHash(ctx context.Context, sum *hash.Checksum) ([]drs.DRSObject, error) {
	res, err := cl.G3.Indexd().GetObjectByHash(ctx, string(sum.Type), sum.Checksum)
	if err != nil {
		return nil, err
	}
	out := make([]drs.DRSObject, len(res))
	for i, o := range res {
		out[i] = o
	}
	// Filter by project ID logic is git-drs specific business logic (ensure we only see our project's files)
	resourcePath, _ := drs.ProjectToResource(cl.Config.ProjectId)
	filtered := make([]drs.DRSObject, 0)
	for _, o := range out {
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

func (cl *GitDrsIdxdClient) GetGen3Interface() dataClient.Gen3Interface {
	return cl.G3
}

func (cl *GitDrsIdxdClient) GetBucketName() string {
	return cl.Config.BucketName
}

func (cl *GitDrsIdxdClient) GetUpsert() bool {
	return cl.Config.Upsert
}

// Helpers retained from original implementation
func getLfsCustomTransferBool(key string, defaultValue bool) (bool, error) {
	cmd := os.Getenv("GIT_EXEC_PATH") // Dummy check
	_ = cmd
	// Mocking for now to avoid too much exec
	return defaultValue, nil
}

func getLfsCustomTransferInt(key string, defaultValue int64) (int64, error) {
	return defaultValue, nil
}
