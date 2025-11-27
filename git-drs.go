package main

import (
	"fmt"
	"os"

	"github.com/calypr/git-drs/cmd"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/projectdir"
)

func main() {

	_, err := drslog.NewLogger(projectdir.DRS_LOG_FILE, true)
	if err != nil {
		fmt.Printf("Failed to open log file: %v", err)
		os.Exit(1)
	}

	if err := cmd.RootCmd.Execute(); err != nil {
		drslog.Close() // closes log file if there was one
		os.Exit(1)
	}
}
