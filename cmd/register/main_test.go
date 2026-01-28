package register_test

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestRegisterCmd(t *testing.T) {
	testutils.TestCmdMain(t, "register")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
