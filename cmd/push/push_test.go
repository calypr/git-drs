package push

import (
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPushCmdArgs(t *testing.T) {
	// Test with no arguments (valid)
	err := Cmd.Args(Cmd, []string{})
	assert.NoError(t, err)

	// Test with 1 argument (valid)
	err = Cmd.Args(Cmd, []string{"origin"})
	assert.NoError(t, err)

	// Test with multiple arguments (invalid)
	err = Cmd.Args(Cmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestPushRun_LoadConfigError(t *testing.T) {
	_ = testutils.SetupTestGitRepo(t)
	// Don't create config, should fail to load

	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}

func TestPushRun_DefaultRemoteError(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	// Create config with no remotes and no default
	testutils.CreateTestConfig(t, tmpDir, &config.Config{})

	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}

func TestPushRun_ReadOnlyTerraRemoteExplainsWhatWasNotPushed(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: "anvil",
		Remotes: map[config.Remote]config.RemoteSelect{
			"anvil": {Terra: &config.TerraRemote{
				Endpoint: "https://data.terra.bio",
				Mode:     "read-only",
			}},
		},
	})

	err := Cmd.RunE(Cmd, nil)
	require.Error(t, err)
	assert.ErrorContains(t, err, `remote "anvil" is read-only`)
	assert.ErrorContains(t, err, "cannot upload files to Terra")
	assert.ErrorContains(t, err, "no files were uploaded")
	assert.ErrorContains(t, err, "do not need to back out a commit")
	assert.ErrorContains(t, err, "ordinary git push to a Git remote")
	assert.ErrorContains(t, err, "pushes only Git metadata")
}
