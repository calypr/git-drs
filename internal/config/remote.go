package config

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	gitauth "github.com/calypr/git-drs/internal/auth"
	"github.com/calypr/git-drs/internal/gitrepo"
	syclient "github.com/calypr/syfon/client"
	syconf "github.com/calypr/syfon/client/conf"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/syfonclient"
)

type DRSRemote interface {
	GetProjectId() string
	GetOrganization() string
	GetEndpoint() string
	GetBucketName() string
	GetStoragePrefix() string
	GetClient(remoteName string, logger *slog.Logger) (*GitContext, error)
}

type GitContext struct {
	Client             syfonclient.SyfonClient
	Requestor          syrequest.Requester
	Organization       string
	ProjectId          string
	BucketName         string
	StoragePrefix      string
	Upsert             bool
	MultiPartThreshold int64
	UploadConcurrency  int
	Logger             *slog.Logger
	Credential         *syconf.Credential
}

type RemoteSelect struct {
	Gen3  *Gen3Remote
	Local *LocalRemote
}

type Gen3Remote struct {
	Endpoint      string `yaml:"endpoint"`
	ProjectID     string `yaml:"project_id"`
	Bucket        string `yaml:"bucket"`
	Organization  string `yaml:"organization"`
	StoragePrefix string `yaml:"storage_prefix"`
}

func (s Gen3Remote) GetProjectId() string     { return s.ProjectID }
func (s Gen3Remote) GetOrganization() string  { return s.Organization }
func (s Gen3Remote) GetEndpoint() string      { return s.Endpoint }
func (s Gen3Remote) GetBucketName() string    { return s.Bucket }
func (s Gen3Remote) GetStoragePrefix() string { return s.StoragePrefix }

func (s Gen3Remote) GetClient(remoteName string, logger *slog.Logger) (*GitContext, error) {
	manager := syconf.NewConfigure(logger)
	cred, err := manager.Load(remoteName)
	if err != nil {
		return nil, err
	}
	if err := gitauth.EnsureValidCredential(context.Background(), cred, logger); err != nil {
		return nil, err
	}
	return newGitContext(*cred, s, logger)
}

type LocalRemote struct {
	BaseURL       string
	ProjectID     string
	Bucket        string
	Organization  string
	StoragePrefix string
	BasicUsername string
	BasicPassword string
}

func (l LocalRemote) GetProjectId() string {
	if l.ProjectID != "" {
		return l.ProjectID
	}
	return "local-project"
}

func (l LocalRemote) GetOrganization() string  { return l.Organization }
func (l LocalRemote) GetEndpoint() string      { return l.BaseURL }
func (l LocalRemote) GetBucketName() string    { return l.Bucket }
func (l LocalRemote) GetStoragePrefix() string { return l.StoragePrefix }

func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger) (*GitContext, error) {
	if username, password, err := gitrepo.GetRemoteBasicAuth(remoteName); err == nil && username != "" && password != "" {
		l.BasicUsername = username
		l.BasicPassword = password
	}

	cred := &syconf.Credential{APIEndpoint: l.BaseURL}
	if l.BasicUsername != "" || l.BasicPassword != "" {
		cred.KeyID = l.BasicUsername
		cred.APIKey = l.BasicPassword
	}

	rawClient, err := syclient.New(l.BaseURL, syclient.WithBasicAuth(cred.KeyID, cred.APIKey))
	if err != nil {
		return nil, err
	}
	reqClient, ok := rawClient.(interface{ Requestor() syrequest.Requester })
	if !ok {
		return nil, fmt.Errorf("syfon client does not expose requestor")
	}
	syfonClient, ok := rawClient.(syfonclient.SyfonClient)
	if !ok {
		return nil, fmt.Errorf("syfon client does not implement syfonclient.SyfonClient")
	}

	return &GitContext{
		Client:        syfonClient,
		Requestor:     reqClient.Requestor(),
		Organization:  l.GetOrganization(),
		ProjectId:     l.GetProjectId(),
		BucketName:    l.GetBucketName(),
		StoragePrefix: l.GetStoragePrefix(),
		Logger:        logger,
		Credential:    cred,
	}, nil
}

func newGitContext(profileConfig syconf.Credential, remote Gen3Remote, logger *slog.Logger) (*GitContext, error) {
	if _, err := url.Parse(profileConfig.APIEndpoint); err != nil {
		return nil, err
	}
	projectID := remote.GetProjectId()
	if projectID == "" {
		return nil, fmt.Errorf("no gen3 project specified")
	}

	scope, err := gitrepo.ResolveBucketScope(
		remote.GetOrganization(),
		projectID,
		remote.GetBucketName(),
		remote.GetStoragePrefix(),
	)
	if err != nil {
		return nil, err
	}

	rawClient, err := syclient.New(profileConfig.APIEndpoint, syclient.WithBearerToken(profileConfig.AccessToken))
	if err != nil {
		return nil, err
	}
	reqClient, ok := rawClient.(interface{ Requestor() syrequest.Requester })
	if !ok {
		return nil, fmt.Errorf("syfon client does not expose requestor")
	}
	syfonClient, ok := rawClient.(syfonclient.SyfonClient)
	if !ok {
		return nil, fmt.Errorf("syfon client does not implement syfonclient.SyfonClient")
	}

	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	return &GitContext{
		Client:             syfonClient,
		Requestor:          reqClient.Requestor(),
		ProjectId:          projectID,
		BucketName:         scope.Bucket,
		Organization:       remote.GetOrganization(),
		StoragePrefix:      scope.Prefix,
		Upsert:             gitrepo.GetGitConfigBool("drs.upsert", false),
		MultiPartThreshold: int64(gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)) * 1024 * 1024,
		UploadConcurrency:  uploadConcurrency,
		Logger:             logger,
		Credential:         &profileConfig,
	}, nil
}

func localRemoteFromGen3(gen3 *Gen3Remote, username string, password string) *LocalRemote {
	return &LocalRemote{
		BaseURL:       gen3.Endpoint,
		ProjectID:     gen3.ProjectID,
		Bucket:        gen3.Bucket,
		Organization:  gen3.Organization,
		StoragePrefix: gen3.StoragePrefix,
		BasicUsername: strings.TrimSpace(username),
		BasicPassword: strings.TrimSpace(password),
	}
}
