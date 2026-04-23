package local

import (
	"fmt"
	"log/slog"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/gitrepo"
	syclient "github.com/calypr/syfon/client"
	syrequest "github.com/calypr/syfon/client/request"
	"github.com/calypr/syfon/client/syfonclient"
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

	return &client.GitContext{
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
