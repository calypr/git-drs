package push

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "push [remote-name]",
	Short: "push local objects to drs server.",
	Long:  "push local objects to drs server. Any local files that do not have drs records are written to a bucket.",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs push --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		myLogger := drslog.GetLogger()
		cfg, err := config.LoadConfig()
		if err != nil {
			myLogger.Printf("Error loading config: %v", err)
			return err
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				myLogger.Printf("Error getting default remote: %v", err)
				return err
			}
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
