package indexd

import (
	"context"
	"log/slog"

	"github.com/calypr/data-client/conf"
	"github.com/calypr/data-client/g3client"
	"github.com/calypr/data-client/logs"
	"github.com/calypr/git-drs/client"
)

// Gen3Server holds Gen3 server config
type Gen3Remote struct {
	Endpoint  string `yaml:"endpoint"`
	ProjectID string `yaml:"project_id"`
	Bucket    string `yaml:"bucket"`
}

func (s Gen3Remote) GetProjectId() string {
	return s.ProjectID
}

func (s Gen3Remote) GetEndpoint() string {
	return s.Endpoint
}

func (s Gen3Remote) GetBucketName() string {
	return s.Bucket
}

func (s Gen3Remote) GetClient(remoteName string, logger *slog.Logger, opts ...g3client.Option) (client.DRSClient, error) {
	manager := conf.NewConfigure(logger)
	cred, err := manager.Load(remoteName)
	if err != nil {
		return nil, err
	}

	gen3Logger := logs.NewGen3Logger(logger, "", remoteName)
	if err := g3client.EnsureValidCredential(context.Background(), cred, manager, gen3Logger, nil); err != nil {
		return nil, err
	}

	return NewGitDrsIdxdClient(*cred, s, logger, opts...)
}
