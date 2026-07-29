package remoteruntime

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
			switch x.Generic.Auth {
			case "bearer", "provider-helper", "provider-helper:gen3-profile":
				return gen3ClientWithCredential(string(remote), x.Generic.Credential, config.Gen3Remote{
					Endpoint: x.Generic.Endpoint, Organization: x.Generic.GetOrganization(),
					ProjectID: x.Generic.GetProjectId(), Bucket: x.Generic.GetBucketName(),
					StoragePrefix: x.Generic.GetStoragePrefix(),
				}, logger)
			default:
				return nil, fmt.Errorf("authentication method %q is not supported by the Gen3 remote adapter", x.Generic.Auth)
			}
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
	return gen3ClientWithCredential(remoteName, "", remote, logger)
}

func gen3ClientWithCredential(remoteName, source string, remote config.Gen3Remote, logger *slog.Logger) (*GitContext, error) {
	manager := calyprconf.NewConfigure(logger)
	cred, saveRefreshed, err := resolveGen3Credential(manager, source, remoteName, remote.Endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := credentials.EnsureValidCredential(ctx, cred, logger); err != nil {
		return nil, WrapCredentialValidationError(remoteName, err)
	}
	if saveRefreshed {
		if err := manager.Save(cred); err != nil {
			return nil, fmt.Errorf("save refreshed credential for remote %q: %w", remoteName, err)
		}
	}
	return newGitContext(*cred, remote, logger)
}

type gen3CredentialManager interface {
	Import(filePath, fenceToken string) (*calyprconf.Credential, error)
	Load(profile string) (*calyprconf.Credential, error)
}

// resolveGen3Credential turns the clone-local source selector into credential
// material. The configured remote endpoint remains authoritative: a credential
// source must not be able to redirect requests to another host.
func resolveGen3Credential(manager gen3CredentialManager, source, remoteName, endpoint string) (*calyprconf.Credential, bool, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		cred, err := manager.Load(remoteName)
		return cred, true, err
	}

	var cred *calyprconf.Credential
	var err error
	switch {
	case strings.HasPrefix(source, "profile:"):
		profile := strings.TrimSpace(strings.TrimPrefix(source, "profile:"))
		cred, err = manager.Load(profile)
		if err == nil {
			cred.Profile = profile
		}
		if err != nil {
			return nil, false, fmt.Errorf("load Gen3 credential profile %q: %w", profile, err)
		}
		cred.APIEndpoint = endpoint
		return cred, true, nil
	case strings.HasPrefix(source, "file:"):
		fileName := strings.TrimSpace(strings.TrimPrefix(source, "file:"))
		if strings.HasPrefix(fileName, "~/") {
			if home, homeErr := os.UserHomeDir(); homeErr == nil {
				fileName = filepath.Join(home, strings.TrimPrefix(fileName, "~/"))
			}
		}
		cred, err = manager.Import(fileName, "")
		if err != nil {
			return nil, false, fmt.Errorf("import Gen3 credential file %q: %w", fileName, err)
		}
	case strings.HasPrefix(source, "env:"):
		name := strings.TrimSpace(strings.TrimPrefix(source, "env:"))
		token, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(token) == "" {
			return nil, false, fmt.Errorf("Gen3 credential environment variable %q is empty or unset", name)
		}
		cred = &calyprconf.Credential{AccessToken: strings.TrimSpace(token)}
	case strings.HasPrefix(source, "helper:"):
		name := strings.TrimSpace(strings.TrimPrefix(source, "helper:"))
		output, helperErr := exec.Command(name).Output()
		if helperErr != nil {
			return nil, false, fmt.Errorf("run Gen3 credential helper %q: %w", name, helperErr)
		}
		token := strings.TrimSpace(string(output))
		if token == "" {
			return nil, false, fmt.Errorf("Gen3 credential helper %q returned an empty token", name)
		}
		cred = &calyprconf.Credential{AccessToken: token}
	default:
		return nil, false, fmt.Errorf("unsupported Gen3 credential source %q", source)
	}

	cred.Profile = remoteName
	cred.APIEndpoint = endpoint
	return cred, false, nil
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
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		scope, err = resolveBucketScopeFromServer(
			ctx,
			profileConfig.APIEndpoint,
			profileConfig.AccessToken,
			remote.GetOrganization(),
			projectID,
			remote.GetBucketName(),
		)
		if err != nil {
			return nil, fmt.Errorf("failed resolving bucket mapping for organization=%q project=%q: %w", remote.GetOrganization(), projectID, err)
		}
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
