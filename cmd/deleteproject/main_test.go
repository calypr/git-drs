package deleteproject_test

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestDeleteProjectCmd(t *testing.T) {
	testutils.TestCmdMain(t, "deleteproject")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
