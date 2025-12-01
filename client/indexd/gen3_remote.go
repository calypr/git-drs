package indexd_client

import (
	"log"

	"github.com/calypr/git-drs/client"
)

// Gen3Server holds Gen3 server config
type Gen3Remote struct {
	Endpoint  string `yaml:"endpoint"`
	ProjectID string `yaml:"project_id"`
	Bucket    string `yaml:"bucket"`
	APIKey    string `yaml:"api_key"`
	KeyID     string `yaml:"key_id"`
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

func (s Gen3Remote) GetAPIKey() string {
	return s.APIKey
}

func (s Gen3Remote) GetKeyId() string {
	return s.KeyID
}

func (s Gen3Remote) GetClient(params map[string]string, logger *log.Logger) (client.DRSClient, error) {
	cred, err := GetJWTCredendial(params)
	if err != nil {
		return nil, err
	}
	return NewIndexDClient(cred, s, params["remote_name"], logger)
}
