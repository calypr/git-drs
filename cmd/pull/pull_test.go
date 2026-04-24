package pull

import (
	"testing"

	"github.com/calypr/git-drs/internal/config"
	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestPullCmdArgs(t *testing.T) {
	err := Cmd.Args(Cmd, []string{})
	assert.NoError(t, err)

	err = Cmd.Args(Cmd, []string{"origin"})
	assert.NoError(t, err)

	err = Cmd.Args(Cmd, []string{"origin", "extra"})
	assert.Error(t, err)
}

func TestPullRun_LoadConfigError(t *testing.T) {
	_ = testutils.SetupTestGitRepo(t)
	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}

func TestPullRun_DefaultRemoteError(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateTestConfig(t, tmpDir, &config.Config{})

	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}
