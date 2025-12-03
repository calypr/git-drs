package main

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		// is this error getting lost when run in a pre-commit or custom transfer hook?
		fmt.Fprintln(os.Stderr, "git-drs error:", err)
		os.Exit(1)
	}
}
