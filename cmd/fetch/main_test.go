package fetch

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestFetchCmd(t *testing.T) {
	testutils.TestCmdMain(t, "fetch")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
