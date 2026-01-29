package anvil_client

import (
	"fmt"
	"log/slog"

	"github.com/calypr/git-drs/client"
)

// AnvilAuth holds authentication info for Anvil
type AnvilAuth struct {
	TerraProject string `yaml:"terra_project"`
}

// AnvilRemote holds Anvil remote config
type AnvilRemote struct {
	Endpoint string    `yaml:"endpoint"`
	Auth     AnvilAuth `yaml:",inline"`
}

func (s AnvilRemote) GetProjectId() string {
	return s.Auth.TerraProject
}

func (s AnvilRemote) GetEndpoint() string {
	return s.Endpoint
}

func (s AnvilRemote) GetBucketName() string {
	return ""
}

func (s AnvilRemote) GetClient(remoteName string, logger *slog.Logger) (client.DRSClient, error) {
	return nil, fmt.Errorf(("AnVIL Client needs to be implemented"))
	// return NewAnvilClient(s, logger)
}
