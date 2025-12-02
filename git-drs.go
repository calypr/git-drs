package main

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "git-drs error:", err)
		os.Exit(1)
	}
}
