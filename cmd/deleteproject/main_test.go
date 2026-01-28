package deleteproject

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestDeleteProjectCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "deleteproject")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
