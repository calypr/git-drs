package smudge

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestSmudgeCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "smudge")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
