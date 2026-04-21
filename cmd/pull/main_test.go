package pull

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestPullCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "pull")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
