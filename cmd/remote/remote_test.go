package remote

import (
	"testing"

	"github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteListArgs(t *testing.T) {
	// Test with no arguments (valid)
	err := ListCmd.Args(ListCmd, []string{})
	assert.NoError(t, err)

	// Test with arguments (invalid)
	err = ListCmd.Args(ListCmd, []string{"extra"})
	assert.Error(t, err)
}

func TestRemoteListRun(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)

	// Capture stdout
	output := testutils.CaptureStdout(t, func() {
		err := ListCmd.RunE(ListCmd, []string{})
		assert.NoError(t, err)
	})

	assert.Contains(t, output, "origin")
	assert.Contains(t, output, "gen3")
}

func TestRemoteSetArgs(t *testing.T) {
	// Test with 1 argument (valid)
	err := SetCmd.Args(SetCmd, []string{"origin"})
	assert.NoError(t, err)

	// Test with no arguments (invalid)
	err = SetCmd.Args(SetCmd, []string{})
	assert.Error(t, err)

	// Test with multiple arguments (invalid)
	err = SetCmd.Args(SetCmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestRemoteRemoveArgs(t *testing.T) {
	err := RemoveCmd.Args(RemoveCmd, []string{"origin"})
	assert.NoError(t, err)

	err = RemoveCmd.Args(RemoveCmd, []string{})
	assert.Error(t, err)

	err = RemoveCmd.Args(RemoveCmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestRemoteRemoveAliases(t *testing.T) {
	assert.Contains(t, RemoveCmd.Aliases, "rm")
}

func TestRemoteRemoveRun(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{
		DefaultRemote: config.Remote("origin"),
		Remotes: map[config.Remote]config.RemoteSelect{
			"origin": {
				Gen3: &indexd.Gen3Remote{Endpoint: "https://one.example", ProjectID: "proj-a", Bucket: "bucket-a"},
			},
			"staging": {
				Gen3: &indexd.Gen3Remote{Endpoint: "https://two.example", ProjectID: "proj-b", Bucket: "bucket-b"},
			},
		},
	})

	err := RemoveCmd.RunE(RemoveCmd, []string{"origin"})
	require.NoError(t, err)

	cfg, err := config.LoadConfig()
	require.NoError(t, err)

	_, hasOrigin := cfg.Remotes[config.Remote("origin")]
	assert.False(t, hasOrigin)
	assert.Equal(t, config.Remote("staging"), cfg.DefaultRemote)
}

func TestRemoteRemoveRunNotFound(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)

	err := RemoveCmd.RunE(RemoveCmd, []string{"missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote 'missing' not found")
}
