package query

import (
	"encoding/json"

	"github.com/calypr/git-drs/config"
	"github.com/calypr/git-drs/log"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "query <drs_id>",
	Short: "Query DRS server by DRS ID",
	Long:  "Query DRS server by DRS ID",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := log.NewLogger("", true)
		if err != nil {
			return err
		}
		config, err := config.LoadConfig()
		if err != nil {
			return err
		}
		client, err := config.GetCurrentRemoteClient(logger)
		if err != nil {
			return err
		}

		obj, err := client.GetObject(args[0])
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(*obj, "", "  ")
		if err != nil {
			return err
		}
		logger.Logf("%s\n", string(out))
		return nil
	},
}
