package push

import (
	"testing"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
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
