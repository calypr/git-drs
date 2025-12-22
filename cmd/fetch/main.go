package fetch

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "fetch [remote-name]",
	Short: "fetch drs objects from remote",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) > 1 {
			cmd.SilenceUsage = false
			return fmt.Errorf("error: accepts at most 1 argument (remote name), received %d\n\nUsage: %s\n\nSee 'git drs fetch --help' for more details", len(args), cmd.UseLine())
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
		}

		var remote config.Remote
		if len(args) > 0 {
			remote = config.Remote(args[0])
		} else {
			remote, err = cfg.GetDefaultRemote()
			if err != nil {
				logger.Printf("Error getting remote: %v", err)
				return err
			}
		}

		drsClient, err := cfg.GetRemoteClient(remote, logger)
		if err != nil {
			logger.Printf("\nerror creating DRS client: %s", err)
			return err
		}

		err = drsmap.PullRemoteDrsObjects(drsClient, logger)
		if err != nil {
			return err
		}

		return nil
	},
}
