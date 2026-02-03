package download

import (
	"testing"

	"github.com/calypr/git-drs/internal/testutils"
)

func TestDownloadCmd(t *testing.T) {
	testutils.RunCmdMainTest(t, "download")
}

func TestValidateArgs(t *testing.T) {
	testutils.RunCmdArgsTest(t)
}
