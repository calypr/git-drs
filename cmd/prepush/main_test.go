package prepush

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestMain(t *testing.T) {
	testutils.TestCmdMain(t, "prepush")
}

func TestValidateArgs(t *testing.T) {
	testutils.TestCmdArgs(t)
}
