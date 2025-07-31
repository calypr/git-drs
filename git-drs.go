package main

import (
	"os"

	"github.com/calypr/git-drs/cmd"
)

func main() {
	if err := cmd.RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
