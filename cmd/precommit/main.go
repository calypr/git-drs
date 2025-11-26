package precommit

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"
	"github.com/calypr/git-drs/drsmap"
	"github.com/calypr/git-drs/log"
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
		myLogger, err := log.NewLogger("", true)
		if err != nil {
			myLogger.Logf("Failed to open log file: %v", err)
			return err
		}
		defer myLogger.Close()

		myLogger.Log("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")

		// get the current server from config and log it
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error getting config: %v", err)
		}
		myLogger.Logf("Current server: %s", cfg.GetCurrentRemoteName())

		client, err := cfg.GetCurrentRemoteClient(myLogger)

		myLogger.Logf("Preparing DRS objects for commit...\n")
		err = drsmap.UpdateDrsObjects(client, myLogger)
		if err != nil {
			myLogger.Log("UpdateDrsObjects failed:", err)
			return err
		}
		myLogger.Logf("DRS objects prepared for commit!\n")

		myLogger.Log("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
		return nil
	},
}
