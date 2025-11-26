package testutils

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/projectdir"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// SetupTestGitRepo creates a temp directory mocking a real git repo
func SetupTestGitRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "git-drs-test-*")
	require.NoError(t, err)

	originalDir, err := os.Getwd()
	require.NoError(t, err)

	err = os.Chdir(tmpDir)
	require.NoError(t, err)

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	err = cmd.Run()
	require.NoError(t, err)

	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	_ = cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	_ = cmd.Run()

	t.Cleanup(func() {
		os.Chdir(originalDir)
		os.RemoveAll(tmpDir)
	})

	return tmpDir
}

// CreateTestConfig creates a test Git DRS config file with the given content
func CreateTestConfig(t *testing.T, tmpDir string, cfg *config.Config) string {
	t.Helper()

	configDir := filepath.Join(tmpDir, projectdir.DRS_DIR)
	err := os.MkdirAll(configDir, 0755)
	require.NoError(t, err)

	configPath := filepath.Join(configDir, projectdir.CONFIG_YAML)
	file, err := os.Create(configPath)
	require.NoError(t, err)
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	err = encoder.Encode(cfg)
	require.NoError(t, err)

	return configPath
}

// CreateDefaultTestConfig creates a standard test configuration
func CreateDefaultTestConfig(t *testing.T, tmpDir string) *config.Config {
	t.Helper()

	testConfig := &config.Config{
		CurrentRemote: "origin",
		Remotes: map[string]config.RemoteSelect{
			"origin": config.RemoteSelect{
				Gen3: &indexd_client.Gen3Remote{
					Endpoint: "https://test.gen3.org",
					Auth: indexd_client.Gen3Auth{
						Profile:   "test-profile",
						ProjectID: "test-program-test-project",
						Bucket:    "test-bucket",
					},
				},
			},
		},
	}

	CreateTestConfig(t, tmpDir, testConfig)
	return testConfig
}
