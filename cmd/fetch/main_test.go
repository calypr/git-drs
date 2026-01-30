package fetch

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestFetchCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "fetch")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
