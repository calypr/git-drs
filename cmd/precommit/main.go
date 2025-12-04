package precommit

import (
	"fmt"

	indexd_client "github.com/calypr/git-drs/client/indexd"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drs"

	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
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
	Use:   "precommit <remote>",
	Short: "pre-commit hook to create DRS objects",
	Long:  "Pre-commit hook that creates and commits a DRS object to the repo for every LFS file committed",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()

		myLogger.Print("~~~~~~~~~~~~~ START: pre-commit ~~~~~~~~~~~~~")

		// get the current server from config and log it
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error getting config: %v", err)
		}

		cli, err := cfg.GetRemoteClient(config.Remote(args[0]), myLogger)

		dc, ok := cli.(*indexd_client.IndexDClient)
		if !ok {
			return fmt.Errorf("cli is not IndexdClient: %s", cli)
		}
		myLogger.Printf("Current server: %s", dc.ProjectId)

		myLogger.Printf("Preparing DRS objects for commit...\n")
		err = drsmap.UpdateDrsObjects(cli, myLogger)
		if err != nil {
			myLogger.Print("UpdateDrsObjects failed:", err)
			return err
		}
		myLogger.Printf("DRS objects prepared for commit!\n")

		myLogger.Print("~~~~~~~~~~~~~ COMPLETED: pre-commit ~~~~~~~~~~~~~")
		return nil
	},
}
