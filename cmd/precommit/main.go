package precommit

import (
	"fmt"
	"log"

	"github.com/bmeg/git-drs/client"
	"github.com/bmeg/git-drs/drs"
	"github.com/spf13/cobra"
)

var (
	server  string
	dstPath string
	drsObj  *drs.DRSObject
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "precommit",
	Short: "pre-commit hook to create DRS objects",
	Long:  "Pre-commit hook that creates and commits a DRS object to the repo for every LFS file committed",
	Args:  cobra.ExactArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		// set up logger
		myLogger, err := client.NewLogger("")
		if err != nil {
			log.Fatalf("Failed to open log file: %v", err)
		}
		defer myLogger.Close()

		myLogger.Log("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")

		// get the current server from config and log it
		cfg, err := client.LoadConfig()
		if err != nil {
			return fmt.Errorf("error getting config: %v", err)
		}
		fmt.Printf("Current server: %s\n", cfg.CurrentServer)
		fmt.Printf("To use another server, unstage your current files and re-run `git drs init` before re-adding files\n")
		myLogger.Log("Current server: %s", cfg.CurrentServer)

		err = client.UpdateDrsObjects()
		if err != nil {
			fmt.Println("UpdateDrsObjects failed:", err)
			log.Fatalf("UpdateDrsObjects failed: %v", err)
			return err
		}

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
		return nil
	},
}
