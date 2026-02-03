package delete

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestDeleteCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "delete")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
