package testutils

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/stretchr/testify/require"
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

// CreateTestConfig applies the given config to the git repository using git config commands
func CreateTestConfig(t *testing.T, tmpDir string, cfg *config.Config) {
	t.Helper()

	// Helper to run git config
	setConfig := func(key, value string) {
		cmd := exec.Command("git", "config", key, value)
		cmd.Dir = tmpDir
		err := cmd.Run()
		require.NoError(t, err, "failed to set git config %s=%s", key, value)
	}

	if cfg.DefaultRemote != "" {
		setConfig("drs.default-remote", string(cfg.DefaultRemote))
	}

	for name, remote := range cfg.Remotes {
		prefix := fmt.Sprintf("drs.remote.%s", name)
		if remote.Gen3 != nil {
			setConfig(prefix+".type", "gen3")
			setConfig(prefix+".endpoint", remote.Gen3.Endpoint)
			setConfig(prefix+".project", remote.Gen3.ProjectID)
			setConfig(prefix+".bucket", remote.Gen3.Bucket)
		} else if remote.Anvil != nil {
			setConfig(prefix+".type", "anvil")
		}
	}
}

// CreateDefaultTestConfig creates a standard test configuration
func CreateDefaultTestConfig(t *testing.T, tmpDir string) *config.Config {
	t.Helper()

	testConfig := &config.Config{
		DefaultRemote: config.Remote(config.ORIGIN),
		Remotes: map[config.Remote]config.RemoteSelect{
			config.Remote(config.ORIGIN): {
				Gen3: &indexd.Gen3Remote{
					Endpoint:  "https://test.gen3.org",
					ProjectID: "test-project",
					Bucket:    "test",
				},
			},
		},
	}

	CreateTestConfig(t, tmpDir, testConfig)
	return testConfig
}
