package listconfig

import (
	"encoding/json"
	"testing"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestListConfigCommand(t *testing.T) {
	t.Run("displays valid config in YAML format", func(t *testing.T) {
		tmpDir := testutils.SetupTestGitRepo(t)
		_ = testutils.CreateDefaultTestConfig(t, tmpDir)

		cmd := &cobra.Command{
			Use:   "list-config",
			Short: "Display the current configuration",
			Args:  cobra.ExactArgs(0),
			RunE:  Cmd.RunE,
		}
		cmd.SetArgs([]string{})

		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		assert.Contains(t, output, "current_server:")
		assert.Contains(t, output, "gen3")
		assert.Contains(t, output, "https://test.gen3.org")

		var parsedConfig config.Config
		err := yaml.Unmarshal([]byte(output), &parsedConfig)
		require.NoError(t, err)

		//TODO: update tests to match interface
		assert.Equal(t, config.Gen3ServerType, parsedConfig.GetCurrentRemoteName())
		//assert.NotNil(t, parsedConfig.Servers.Gen3)
		//assert.Equal(t, "https://test.gen3.org", parsedConfig.Servers.Gen3.Endpoint)
	})

	t.Run("displays valid config in JSON format", func(t *testing.T) {
		//tmpDir := testutils.SetupTestGitRepo(t)
		//testConfig := testutils.CreateDefaultTestConfig(t, tmpDir)

		cmd := &cobra.Command{
			Use:   "list-config",
			Short: "Display the current configuration",
			Args:  cobra.ExactArgs(0),
			RunE:  Cmd.RunE,
		}
		cmd.SetArgs([]string{"--json"})

		// Add the json flag to the command
		cmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output in JSON format")

		output := testutils.CaptureStdout(t, func() {
			err := cmd.Execute()
			require.NoError(t, err)
		})

		// Verify it's valid JSON by unmarshaling
		var parsedConfig config.Config
		err := json.Unmarshal([]byte(output), &parsedConfig)
		require.NoError(t, err)

		//TODO: update tests to match new interface
		// Verify the content matches expected values
		// assert.Equal(t, config.Gen3ServerType, parsedConfig.CurrentServer)
		// assert.NotNil(t, parsedConfig.Servers.Gen3)
		// assert.Equal(t, testConfig.Servers.Gen3.Endpoint, parsedConfig.Servers.Gen3.Endpoint)
		// assert.Equal(t, testConfig.Servers.Gen3.Auth.Profile, parsedConfig.Servers.Gen3.Auth.Profile)
		// assert.Equal(t, testConfig.Servers.Gen3.Auth.ProjectID, parsedConfig.Servers.Gen3.Auth.ProjectID)
		// assert.Equal(t, testConfig.Servers.Gen3.Auth.Bucket, parsedConfig.Servers.Gen3.Auth.Bucket)
	})

	t.Run("returns error when config file does not exist", func(t *testing.T) {
		testutils.SetupTestGitRepo(t)

		cmd := &cobra.Command{
			Use:   "list-config",
			Short: "Display the current configuration",
			Args:  cobra.ExactArgs(0),
			RunE:  Cmd.RunE,
		}

		err := cmd.Execute()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "config file does not exist")
	})
}
