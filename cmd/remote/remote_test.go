package remote

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
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
