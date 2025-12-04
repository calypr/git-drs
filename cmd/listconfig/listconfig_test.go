package listconfig

/* This test isn't really needed because there is no concept of a current remote anymore
func TestListConfigCommand(t *testing.T) {
	t.Run("displays valid config in YAML format", func(t *testing.T) {
		tmpDir := testutils.SetupTestGitRepo(t)
		testConfig := testutils.CreateDefaultTestConfig(t, tmpDir)

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

		var parsedConfig config.Config
		err := yaml.Unmarshal([]byte(output), &parsedConfig)
		require.NoError(t, err)

		// ensure config used to set is same as parsed config from stdout
		verifyParsedConfig(t, parsedConfig, testConfig)
	})

	t.Run("displays valid config in JSON format", func(t *testing.T) {
		tmpDir := testutils.SetupTestGitRepo(t)
		testConfig := testutils.CreateDefaultTestConfig(t, tmpDir)

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

		// Verify the content matches expected values
		verifyParsedConfig(t, parsedConfig, testConfig)
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

// helper method
func verifyParsedConfig(t *testing.T, parsedConfig config.Config, testConfig *config.Config) {
	assert.Equal(t, testConfig.GetCurrentRemoteName(), parsedConfig.GetCurrentRemoteName())
	assert.Equal(t, testConfig.GetCurrentRemote().GetEndpoint(), parsedConfig.GetCurrentRemote().GetEndpoint())
	assert.Equal(t, testConfig.GetCurrentRemote().GetProjectId(), parsedConfig.GetCurrentRemote().GetProjectId())
	assert.Equal(t, testConfig.GetCurrentRemote().GetBucketName(), parsedConfig.GetCurrentRemote().GetBucketName())
	assert.Equal(t, testConfig.GetCurrentRemote().GetEndpoint(), parsedConfig.GetCurrentRemote().GetEndpoint())
}
*/
