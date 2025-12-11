package push

import (
	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "push",
	Short: "push local objects to drs server.",
	Long:  "push local objects to drs server. Any local files that do not have drs records are written to a bucket.",
	RunE: func(cmd *cobra.Command, args []string) error {
		var remote config.Remote = config.ORIGIN
		if len(args) > 0 {
			remote = config.Remote(args[0])
		}

		myLogger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Printf("Error loading config: %v", err)
			return err
		}

		drsClient, err := cfg.GetRemoteClient(remote, myLogger)
		if err != nil {
			myLogger.Printf("Error creating indexd client: %s", err)
			return err
		}

		err = drsmap.PushLocalDrsObjects(drsClient, myLogger)
		if err != nil {
			return err
		}

		return nil
	},
}
