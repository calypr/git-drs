package delete

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestDeleteCmdArgs(t *testing.T) {
	// Test with 2 arguments (valid)
	err := Cmd.Args(Cmd, []string{"sha256", "oid"})
	assert.NoError(t, err)

	// Test with 1 argument (invalid)
	err = Cmd.Args(Cmd, []string{"sha256"})
	assert.Error(t, err)

	// Test with 3 arguments (invalid)
	err = Cmd.Args(Cmd, []string{"sha256", "oid", "extra"})
	assert.Error(t, err)
}

func TestDeleteRun_Error(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	// No config
	err := Cmd.RunE(Cmd, []string{"sha256", "oid"})
	assert.Error(t, err)

	// Invalid hash type
	testutils.CreateDefaultTestConfig(t, tmpDir)
	err = Cmd.RunE(Cmd, []string{"md5", "oid"})
	assert.Error(t, err)
}
