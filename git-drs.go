package main

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd"
	"github.com/calypr/git-drs/drslog"
)

func main() {

	_, err := drslog.NewLogger("", true)
	if err != nil {
		fmt.Printf("Failed to open log file: %v", err)
		os.Exit(1)
	}

	if err := cmd.RootCmd.Execute(); err != nil {
		drslog.Close() // closes log file if there was one
		os.Exit(1)
	}
}
