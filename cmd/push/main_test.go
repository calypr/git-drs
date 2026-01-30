package push

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestPushCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "push")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
