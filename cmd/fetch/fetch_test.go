package fetch

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestFetchCmdArgs(t *testing.T) {
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

func TestFetchRun_Error(t *testing.T) {
	_ = testutils.SetupTestGitRepo(t)
	// No config, should error
	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}

func TestFetchRun_InvalidRemote(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)
	// Fetch from non-existent remote
	err := Cmd.RunE(Cmd, []string{"no-remote"})
	assert.Error(t, err)
}
