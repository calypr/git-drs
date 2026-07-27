package remoteruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	calyprconf "github.com/calypr/calypr-cli/conf"
	"github.com/calypr/calypr-cli/credentials"
	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/gitrepo"
	syclient "github.com/calypr/syfon/client"
	syconf "github.com/calypr/syfon/client/config"
)

const credentialHelpSuffix = "Refresh credentials with `git drs remote add gen3 <remote-name> <organization/project> --cred <path>` or `--token <token>`. See docs/getting-started.md."

type GitContext struct {
	Client             *syclient.Client
	RemoteType         config.RemoteType
	Endpoint           string
	Organization       string
	ProjectId          string
	BucketName         string
	StoragePrefix      string
	Upsert             bool
	ForceUpload        bool
	MultiPartThreshold int64
	UploadConcurrency  int
	Logger             *slog.Logger
	Credential         *syconf.Credential
	Capabilities       Capabilities
}

// Capabilities is the command-facing contract for a resolved remote. Commands
// must use this contract rather than infer behaviour from configuration types.
type Capabilities struct {
	Resolve, Download, Upload, Register, ReadOnly bool
}

func (g *GitContext) CanResolve() bool  { return g != nil && g.Capabilities.Resolve }
func (g *GitContext) CanDownload() bool { return g != nil && g.Capabilities.Download }
func (g *GitContext) CanUpload() bool   { return g != nil && g.Capabilities.Upload }
func (g *GitContext) CanRegister() bool { return g != nil && g.Capabilities.Register }
func (g *GitContext) IsReadOnly() bool  { return g != nil && g.Capabilities.ReadOnly }

func New(cfg *config.Config, remote config.Remote, logger *slog.Logger) (*GitContext, error) {
	x, ok := cfg.Remotes[remote]
	if !ok {
		return nil, fmt.Errorf("GetRemoteClient no remote configuration found for current remote: %s", remote)
	}
	if x.Local != nil {
		return localClient(string(remote), *x.Local, logger)
	}
	if x.Gen3 != nil {
		username, password, err := gitrepo.GetRemoteBasicAuth(string(remote))
		if err == nil && strings.TrimSpace(username) != "" && strings.TrimSpace(password) != "" {
			return localClient(string(remote), *localRemoteFromGen3(x.Gen3, username, password), logger)
		}
		return gen3Client(string(remote), *x.Gen3, logger)
	}
	if x.Terra != nil {
		return terraClient(*x.Terra, logger)
	}
	if x.Generic != nil {
		switch x.Generic.Provider {
		case "terra":
			return terraClient(config.TerraRemote{Endpoint: x.Generic.Endpoint, Auth: x.Generic.Auth, Mode: "read-only"}, logger)
		case "gen3":
			return gen3Client(string(remote), config.Gen3Remote{
				Endpoint: x.Generic.Endpoint, Organization: x.Generic.GetOrganization(),
				ProjectID: x.Generic.GetProjectId(), Bucket: x.Generic.GetBucketName(),
				StoragePrefix: x.Generic.GetStoragePrefix(),
			}, logger)
		case "ga4gh", "auto", "cgc", "synapse":
			return nil, fmt.Errorf("provider %q does not yet have an operational remote adapter", x.Generic.Provider)
		default:
			return nil, fmt.Errorf("unsupported remote provider %q", x.Generic.Provider)
		}
	}
	return nil, fmt.Errorf("no valid remote configuration found for current remote: %s", remote)
}

func terraClient(remote config.TerraRemote, logger *slog.Logger) (*GitContext, error) {
	if strings.TrimSpace(remote.Endpoint) == "" {
		return nil, fmt.Errorf("no terra endpoint specified")
	}
	if _, err := url.Parse(remote.Endpoint); err != nil {
		return nil, err
	}
	return &GitContext{
		RemoteType:   config.TerraServerType,
		Endpoint:     remote.Endpoint,
		Logger:       logger,
		Credential:   &syconf.Credential{APIEndpoint: remote.Endpoint},
		Capabilities: Capabilities{Resolve: true, Download: true, ReadOnly: true},
	}, nil
}

func gen3Client(remoteName string, remote config.Gen3Remote, logger *slog.Logger) (*GitContext, error) {
	manager := calyprconf.NewConfigure(logger)
	cred, err := manager.Load(remoteName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := credentials.EnsureValidCredential(ctx, cred, logger); err != nil {
		return nil, WrapCredentialValidationError(remoteName, err)
	}
	if err := manager.Save(cred); err != nil {
		return nil, fmt.Errorf("save refreshed credential for remote %q: %w", remoteName, err)
	}
	return newGitContext(*cred, remote, logger)
}

func localClient(remoteName string, remote config.LocalRemote, logger *slog.Logger) (*GitContext, error) {
	if username, password, err := gitrepo.GetRemoteBasicAuth(remoteName); err == nil && username != "" && password != "" {
		remote.BasicUsername = username
		remote.BasicPassword = password
	}
	projectID := remote.GetProjectId()
	bucketName := remote.GetBucketName()
	storagePrefix := remote.GetStoragePrefix()
	if strings.TrimSpace(remote.GetOrganization()) != "" || strings.TrimSpace(bucketName) != "" || strings.TrimSpace(storagePrefix) != "" {
		scope, err := gitrepo.ResolveBucketScope(
			remote.GetOrganization(),
			projectID,
			bucketName,
			storagePrefix,
		)
		if err != nil {
			return nil, err
		}
		bucketName = scope.Bucket
		storagePrefix = scope.Prefix
	}

	cred := &syconf.Credential{APIEndpoint: remote.BaseURL}
	if remote.BasicUsername != "" || remote.BasicPassword != "" {
		cred.KeyID = remote.BasicUsername
		cred.APIKey = remote.BasicPassword
	}

	raw, err := syclient.New(remote.BaseURL, syclient.WithBasicAuth(cred.KeyID, cred.APIKey))
	if err != nil {
		return nil, err
	}
	client, ok := raw.(*syclient.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected syfon client type %T", raw)
	}

	return &GitContext{
		Client:        client,
		RemoteType:    config.LocalServerType,
		Endpoint:      remote.BaseURL,
		Organization:  remote.GetOrganization(),
		ProjectId:     projectID,
		BucketName:    bucketName,
		StoragePrefix: storagePrefix,
		Logger:        logger,
		Credential:    cred,
		Capabilities:  Capabilities{Resolve: true, Download: true, Upload: true, Register: true},
	}, nil
}

func newGitContext(profileConfig syconf.Credential, remote config.Gen3Remote, logger *slog.Logger) (*GitContext, error) {
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

	raw, err := syclient.New(profileConfig.APIEndpoint, syclient.WithBearerToken(profileConfig.AccessToken))
	if err != nil {
		return nil, err
	}
	client, ok := raw.(*syclient.Client)
	if !ok {
		return nil, fmt.Errorf("unexpected syfon client type %T", raw)
	}

	uploadConcurrency := int(gitrepo.GetGitConfigInt("lfs.concurrenttransfers", 4))
	if uploadConcurrency < 1 {
		uploadConcurrency = 1
	}

	return &GitContext{
		Client:             client,
		RemoteType:         config.Gen3ServerType,
		Endpoint:           profileConfig.APIEndpoint,
		ProjectId:          projectID,
		BucketName:         scope.Bucket,
		Organization:       remote.GetOrganization(),
		StoragePrefix:      scope.Prefix,
		Upsert:             gitrepo.GetGitConfigBool("drs.upsert", false),
		MultiPartThreshold: int64(gitrepo.GetGitConfigInt("drs.multipart-threshold", 5120)) * 1024 * 1024,
		UploadConcurrency:  uploadConcurrency,
		Logger:             logger,
		Credential:         &profileConfig,
		Capabilities:       Capabilities{Resolve: true, Download: true, Upload: true, Register: true},
	}, nil
}

func localRemoteFromGen3(gen3 *config.Gen3Remote, username string, password string) *config.LocalRemote {
	return &config.LocalRemote{
		BaseURL:       gen3.Endpoint,
		ProjectID:     gen3.ProjectID,
		Bucket:        gen3.Bucket,
		Organization:  gen3.Organization,
		StoragePrefix: gen3.StoragePrefix,
		BasicUsername: strings.TrimSpace(username),
		BasicPassword: strings.TrimSpace(password),
	}
}

func WrapCredentialValidationError(remoteName string, err error) error {
	if err == nil {
		return nil
	}
	if strings.TrimSpace(remoteName) == "" {
		return fmt.Errorf("%w. %s", err, credentialHelpSuffix)
	}
	return fmt.Errorf("%w. Remote %q requires refreshed credentials. %s", err, remoteName, credentialHelpSuffix)
}
