package fetch

import (
	"fmt"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/drslog"
	"github.com/calypr/git-drs/drsmap"
	"github.com/spf13/cobra"
)

// Cmd line declaration
// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "fetch [optional_remote]",
	Short: "fetch drs objects from remote",
	Args:  cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := drslog.GetLogger()

		var remote config.Remote = config.ORIGIN
		if len(args) > 0 {
			remote = config.Remote(args[0])
		}

		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("error loading config: %v", err)
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
