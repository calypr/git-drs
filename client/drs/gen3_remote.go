package drs

import (
	"context"
	"log/slog"

	gitauth "github.com/calypr/git-drs/auth"
	"github.com/calypr/git-drs/client"
	"github.com/calypr/syfon/client/conf"
)

// Gen3Server holds Gen3 server config
type Gen3Remote struct {
	Endpoint      string `yaml:"endpoint"`
	ProjectID     string `yaml:"project_id"`
	Bucket        string `yaml:"bucket"`
	Organization  string `yaml:"organization"`
	StoragePrefix string `yaml:"storage_prefix"`
}

func (s Gen3Remote) GetProjectId() string {
	return s.ProjectID
}

func (s Gen3Remote) GetOrganization() string {
	return s.Organization
}

func (s Gen3Remote) GetEndpoint() string {
	return s.Endpoint
}

func (s Gen3Remote) GetBucketName() string {
	return s.Bucket
}

func (s Gen3Remote) GetStoragePrefix() string {
	return s.StoragePrefix
}

func (s Gen3Remote) GetClient(remoteName string, logger *slog.Logger) (*client.GitContext, error) {
	manager := conf.NewConfigure(logger)
	cred, err := manager.Load(remoteName)
	if err != nil {
		return nil, err
	}

	if err := gitauth.EnsureValidCredential(context.Background(), cred, logger); err != nil {
		return nil, err
	}

	return GetContext(*cred, s, logger)
}
