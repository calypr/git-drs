package push

import (
	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/spf13/cobra"
)

var (
	drsClient client.DRSClient
)

var Cmd = &cobra.Command{
	Use:   "push",
	Short: "push local objects to drs server.",
	Long:  "push local objects to drs server. Any local files that do not have drs records are written to a bucket.",
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Printf("Error loading config: %v", err)
			return err
		}

		var remoteName string
		drsClient, err = cfg.GetRemoteClient(config.Remote(remoteName), myLogger)
		if err != nil {
			myLogger.Printf("Error creating indexd client: %s", err)
			return err
		}

		// Gather all objects in .drs/lfs/objects store

		// Write objects that don't exist in remote to remote

		// If object doesn't exist remote but exists locally, upload file
		return nil
	},
}
