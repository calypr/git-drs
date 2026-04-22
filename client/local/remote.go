package local

import (
	"log/slog"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/gitrepo"
)

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

func (l LocalRemote) GetOrganization() string { return l.Organization }
func (l LocalRemote) GetEndpoint() string     { return l.BaseURL }
func (l LocalRemote) GetBucketName() string   { return l.Bucket }
func (l LocalRemote) GetStoragePrefix() string {
	return l.StoragePrefix
}

func (l LocalRemote) GetClient(remoteName string, logger *slog.Logger) (*client.GitContext, error) {
	if username, password, err := gitrepo.GetRemoteBasicAuth(remoteName); err == nil && username != "" && password != "" {
		l.BasicUsername = username
		l.BasicPassword = password
	}

	cred := &conf.Credential{APIEndpoint: l.BaseURL}
	if l.BasicUsername != "" || l.BasicPassword != "" {
		cred.KeyID = l.BasicUsername
		cred.APIKey = l.BasicPassword
	}

	dLogger, closer := logs.New("local", logs.WithBaseLogger(logger), logs.WithNoConsole(), logs.WithNoMessageFile())
	_ = closer
	api := g3client.NewGen3InterfaceFromCredential(cred, dLogger, g3client.WithClients(g3client.SyfonClient))

	return &client.GitContext{
		API:           api,
		Organization:  l.GetOrganization(),
		ProjectId:     l.GetProjectId(),
		BucketName:    l.GetBucketName(),
		StoragePrefix: l.GetStoragePrefix(),
		Logger:        logger,
		Credential:    cred,
	}, nil
}
