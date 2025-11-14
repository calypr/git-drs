package query

import (
	"encoding/json"

	"github.com/calypr/git-drs/client"
	"github.com/calypr/git-drs/config"
	"github.com/spf13/cobra"
)

// Cmd line declaration
var Cmd = &cobra.Command{
	Use:   "query <remote> <drs_id>",
	Short: "Query DRS server by DRS ID",
	Long:  "Query DRS server by DRS ID",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := client.NewLogger("", true)
		if err != nil {
			return err
		}
		client, err := client.NewIndexDClient(logger, config.Profile(args[0]))
		if err != nil {
			return err
		}

		obj, err := client.GetObject(args[1])
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
