package indexd_client

import (
	"log/slog"

	"github.com/calypr/data-client/conf"
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

func (s Gen3Remote) GetClient(params map[string]string, logger *slog.Logger) (client.DRSClient, error) {
	cred, err := conf.NewConfigure(logger).Load(params["remote_name"])
	if err != nil {
		return nil, err
	}
	return NewGitDrsIdxdClient(*cred, s, logger)
}
