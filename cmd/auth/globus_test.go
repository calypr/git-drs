package auth

import "testing"

func TestGlobusCommandDoesNotExposeCLILoginFlag(t *testing.T) {
	if GlobusCmd.Flag("login") != nil {
		t.Fatal("did not expect CLI-backed --login flag")
	}
}
