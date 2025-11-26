package indexd_client

import (
	"log"

	"github.com/calypr/git-drs/client"
)

// Gen3Auth holds authentication info for Gen3
type Gen3Auth struct {
	Profile   string `yaml:"profile"`
	ProjectID string `yaml:"project_id"`
	Bucket    string `yaml:"bucket"`
}

// Gen3Server holds Gen3 server config
type Gen3Remote struct {
	Endpoint string   `yaml:"endpoint"`
	Auth     Gen3Auth `yaml:",inline"`
}

func (s Gen3Remote) GetProjectId() string {
	return s.Auth.ProjectID
}

func (s Gen3Remote) GetEndpoint() string {
	return s.Endpoint
}

func (s Gen3Remote) GetBucketName() string {
	return s.Auth.Bucket
}

func (s Gen3Remote) GetClient(params map[string]string, logger *log.Logger) (client.DRSClient, error) {
	return NewIndexDClient(s, logger)
}
