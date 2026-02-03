package list

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestListCmdArgs(t *testing.T) {
	err := Cmd.Args(Cmd, []string{})
	assert.NoError(t, err)

	err = Cmd.Args(Cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestListProjectCmdArgs(t *testing.T) {
	err := ListProjectCmd.Args(ListProjectCmd, []string{"project-id"})
	assert.NoError(t, err)

	err = ListProjectCmd.Args(ListProjectCmd, []string{})
	assert.Error(t, err)
}

func TestListRun_Error(t *testing.T) {
	_ = testutils.SetupTestGitRepo(t)
	// No config
	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}

func TestListRun_InvalidRemote(t *testing.T) {
	tmpDir := testutils.SetupTestGitRepo(t)
	testutils.CreateDefaultTestConfig(t, tmpDir)
	remote = "invalid"
	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}
