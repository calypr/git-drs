package transfer

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestTransferCmd(t *testing.T) {
	testutils.TestCmdMain(t, "transfer")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
