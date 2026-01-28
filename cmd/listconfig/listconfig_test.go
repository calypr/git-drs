package listconfig

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestListConfigCmdArgs(t *testing.T) {
	err := Cmd.Args(Cmd, []string{})
	assert.NoError(t, err)

	err = Cmd.Args(Cmd, []string{"extra"})
	assert.Error(t, err)
}

func TestListConfigRun_Error(t *testing.T) {
	_ = testutils.SetupTestGitRepo(t)
	// No config
	err := Cmd.RunE(Cmd, []string{})
	assert.Error(t, err)
}
