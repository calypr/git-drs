package add

import (
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddTerraRemoteCommand(t *testing.T) {
	assert.Equal(t, "terra <remote-name>", TerraCmd.Use)
	assert.NotNil(t, TerraCmd.Flag("drs-endpoint"))
	assert.NotNil(t, TerraCmd.Flag("auth"))
	assert.NotNil(t, TerraCmd.Flag("mode"))
}

func TestValidateTerraOptions(t *testing.T) {
	endpoint, err := validateTerraOptions("https://data.terra.bio", "google-adc", "read-only")
	require.NoError(t, err)
	assert.Equal(t, "https://data.terra.bio", endpoint)

	for name, options := range map[string][3]string{
		"missing endpoint": {"", "google-adc", "read-only"},
		"HTTP endpoint":    {"http://data.terra.bio", "google-adc", "read-only"},
		"URL credentials":  {"https://user:pass@data.terra.bio", "google-adc", "read-only"},
		"unsupported auth": {"https://data.terra.bio", "token", "read-only"},
		"writable mode":    {"https://data.terra.bio", "google-adc", "write"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := validateTerraOptions(options[0], options[1], options[2])
			assert.Error(t, err)
		})
	}
}

func TestTerraRemoteAddPersistsConfig(t *testing.T) {
	testutils.SetupTestGitRepo(t)
	terraEndpoint = "https://data.terra.bio"
	terraAuth = "google-adc"
	terraMode = "read-only"
	t.Cleanup(func() {
		terraEndpoint = ""
		terraAuth = ""
		terraMode = ""
	})

	require.NoError(t, TerraCmd.RunE(TerraCmd, []string{"anvil"}))
	cfg, err := config.LoadConfig()
	require.NoError(t, err)
	remote := cfg.Remotes[config.Remote("anvil")].Terra
	require.NotNil(t, remote)
	assert.Equal(t, "https://data.terra.bio", remote.Endpoint)
	assert.Equal(t, "google-adc", remote.Auth)
	assert.Equal(t, "read-only", remote.Mode)
	assert.Equal(t, config.Remote("anvil"), cfg.DefaultRemote)
}
