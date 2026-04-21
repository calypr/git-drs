package main

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd"
	"github.com/calypr/git-drs/cmd/credentialhelper"
	"github.com/calypr/git-drs/drslog"
)

func main() {

	_, err := drslog.NewLogger("", true)
	if err != nil {
		fmt.Printf("Failed to open log file: %v", err)
		os.Exit(1)
	}

	// Keep credential helper out of the user-facing command tree/help output.
	// Git invokes this path via `credential.helper=!git drs credential-helper`.
	if len(os.Args) > 1 && os.Args[1] == "credential-helper" {
		credentialhelper.Cmd.SetArgs(os.Args[2:])
		if err := credentialhelper.Cmd.Execute(); err != nil {
			drslog.Close()
			os.Exit(1)
		}
		return
	}

	if err := cmd.RootCmd.Execute(); err != nil {
		drslog.Close() // closes log file if there was one
		os.Exit(1)
	}
}
