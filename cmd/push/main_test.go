package push_test

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestPushCmd(t *testing.T) {
	testutils.TestCmdMain(t, "push")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
